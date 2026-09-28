package httpapi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"telcopulse/services/api-gateway/internal/store"
	"telcopulse/services/shared/domain"
	"telcopulse/services/shared/rpc"
)

type notificationRepo struct {
	Repository
	transaction domain.Transaction
	err         error
	calls       int
}

func (r *notificationRepo) Transaction(_ context.Context, _ string) (domain.Transaction, error) {
	r.calls++
	return r.transaction, r.err
}

func TestNotificationStatusProjection(t *testing.T) {
	id := "TXN-0123456789abcdef01234567"
	var upstreamStatus atomic.Int32
	upstreamStatus.Store(404)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer service-token" || r.URL.Path != "/internal/notifications/"+id {
			t.Errorf("unexpected internal request %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		status := int(upstreamStatus.Load())
		w.WriteHeader(status)
		if status == 200 {
			_, _ = io.WriteString(w, `{"transaction_id":"`+id+`","status":"DELIVERED","delivered_at":"2026-09-28T00:00:00Z"}`)
		} else {
			_, _ = io.WriteString(w, `{ "error":"missing" }`)
		}
	}))
	defer upstream.Close()
	repo := &notificationRepo{transaction: domain.Transaction{ID: id, Status: "SUCCESS"}}
	s := Server{Repo: repo, Notifications: rpc.New(upstream.URL, "service-token"), Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	read := func(path string) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		return w
	}
	if w := read("/api/v1/transactions/not-valid/notification"); w.Code != 400 || repo.calls != 0 {
		t.Fatalf("invalid id reached repository: %d", w.Code)
	}
	repo.err = store.ErrNotFound
	if w := read("/api/v1/transactions/" + id + "/notification"); w.Code != 404 {
		t.Fatalf("unknown transaction status=%d", w.Code)
	}
	repo.err = nil
	repo.transaction.Status = "PROCESSING"
	if w := read("/api/v1/transactions/" + id + "/notification"); w.Code != 200 || !strings.Contains(w.Body.String(), `"PROCESSING"`) {
		t.Fatalf("processing status=%d body=%s", w.Code, w.Body)
	}
	repo.transaction.Status = "SUCCESS"
	if w := read("/api/v1/transactions/" + id + "/notification"); w.Code != 200 || !strings.Contains(w.Body.String(), `"AWAITING_DELIVERY"`) {
		t.Fatalf("awaiting status=%d body=%s", w.Code, w.Body)
	}
	upstreamStatus.Store(200)
	if w := read("/api/v1/transactions/" + id + "/notification"); w.Code != 200 || !strings.Contains(w.Body.String(), `"DELIVERED"`) || !strings.Contains(w.Body.String(), time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC).Format(time.RFC3339)) {
		t.Fatalf("delivered status=%d body=%s", w.Code, w.Body)
	}
	upstreamStatus.Store(503)
	if w := read("/api/v1/transactions/" + id + "/notification"); w.Code != 503 || strings.Contains(w.Body.String(), "AWAITING_DELIVERY") {
		t.Fatalf("upstream failure status=%d body=%s", w.Code, w.Body)
	}
	repo.err = errors.New("transaction storage unavailable")
	if w := read("/api/v1/transactions/" + id + "/notification"); w.Code != 503 {
		t.Fatalf("storage failure status=%d", w.Code)
	}
}

func TestDeadLetterReplayGatewayBoundary(t *testing.T) {
	path := "/api/v1/notifications/dead-letters/0/301/replay"
	for _, role := range []string{"Viewer", "Operator", "Incident Commander"} {
		if allowedMutation(role, http.MethodPost, path) {
			t.Fatalf("%s can replay a dead letter", role)
		}
	}
	for _, role := range []string{"Engineer", "Administrator"} {
		if !allowedMutation(role, http.MethodPost, path) {
			t.Fatalf("%s cannot replay a dead letter", role)
		}
	}
	var calls atomic.Int32
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/internal/notifications/dead-letters/0/301/replay" || r.Header.Get("Authorization") != "Bearer internal-test-token" || r.Header.Get("X-Telcopulse-Mutation-Token") != "notification-operator-test-capability" || r.Header.Get("X-Operator-Actor") != "operator:USR-0123456789abcdef01234567" || r.Header.Get("X-Operator-Role") != "Engineer" || r.Header.Get("Idempotency-Key") != "gateway-dead-letter-replay-key" {
			t.Errorf("incorrect replay boundary request %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(201)
		_, _ = io.WriteString(w, `{"status":"REJECTED"}`)
	}))
	defer remote.Close()
	auth := &testAuth{user: Operator{ID: "USR-0123456789abcdef01234567", Username: "engineer", Role: "Viewer"}}
	client := rpc.New(remote.URL, "internal-test-token")
	client.MutationToken = "notification-operator-test-capability"
	s := Server{Auth: auth, RequireAuth: true, Origin: "http://localhost:3000", Notifications: client, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	send := func(origin, key string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(http.MethodPost, path, nil)
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		r.Header.Set("Idempotency-Key", key)
		r.Header.Set("X-Operator-Actor", "operator:USR-ffffffffffffffffffffffff")
		r.AddCookie(&http.Cookie{Name: sessionCookie, Value: strings.Repeat("a", 64)})
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	if got := send("", "gateway-dead-letter-replay-key"); got.Code != 403 || calls.Load() != 0 {
		t.Fatal("missing same-origin proof reached replay")
	}
	if got := send(s.Origin, "gateway-dead-letter-replay-key"); got.Code != 403 || calls.Load() != 0 {
		t.Fatal("viewer reached replay")
	}
	auth.user.Role = "Engineer"
	if got := send(s.Origin, "short"); got.Code != 400 || calls.Load() != 0 {
		t.Fatal("invalid replay key reached service")
	}
	if got := send(s.Origin, "gateway-dead-letter-replay-key"); got.Code != 201 || calls.Load() != 1 || !strings.Contains(got.Body.String(), "REJECTED") {
		t.Fatalf("engineer replay status=%d body=%s", got.Code, got.Body.String())
	}
}

func TestDeadLetterReplayHistoryGatewayRead(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodGet || r.URL.Path != "/internal/notifications/dead-letters/0/301/replays" || r.URL.Query().Get("limit") != "10" || r.Header.Get("Authorization") != "Bearer history-service-token" || r.Header.Get("X-Telcopulse-Mutation-Token") != "" {
			t.Errorf("incorrect history boundary request %s %s", r.Method, r.URL.String())
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"items":[{"status":"REJECTED"}],"more":false}`)
	}))
	defer upstream.Close()
	auth := &testAuth{user: Operator{ID: "USR-0123456789abcdef01234567", Username: "viewer", Role: "Viewer"}}
	s := Server{Auth: auth, RequireAuth: true, Notifications: rpc.New(upstream.URL, "history-service-token"), Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	read := func(path string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, path, nil)
		r.AddCookie(&http.Cookie{Name: sessionCookie, Value: strings.Repeat("a", 64)})
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	if got := read("/api/v1/notifications/dead-letters/0/301/replays?limit=10"); got.Code != 200 || !strings.Contains(got.Body.String(), "REJECTED") {
		t.Fatalf("viewer history response: %d %s", got.Code, got.Body.String())
	}
	for _, path := range []string{
		"/api/v1/notifications/dead-letters/-1/301/replays",
		"/api/v1/notifications/dead-letters/0/301/replays?limit=100",
		"/api/v1/notifications/dead-letters/0/301/replays?raw=true",
	} {
		if got := read(path); got.Code != 400 && got.Code != 422 {
			t.Fatalf("invalid history query accepted: %s -> %d", path, got.Code)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("invalid history query reached source %d times", calls.Load())
	}
}
