package httpapi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"telcopulse/services/api-gateway/internal/workflow"
	"telcopulse/services/shared/domain"
	"testing"
	"time"
)

type fakeRepo struct {
	Repository
	calls   int
	pending bool
	err     error
}

func (f *fakeRepo) Purchase(_ context.Context, p domain.Purchase, _ string) (domain.Transaction, bool, error) {
	f.calls++
	if f.err != nil {
		return domain.Transaction{}, false, f.err
	}
	if f.pending {
		return domain.Transaction{ID: "TXN-pending", Status: "PROCESSING"}, false, workflow.ErrPending
	}
	return domain.Transaction{ID: "test", Status: "FAILED", ErrorCode: "INSUFFICIENT_BALANCE", Environment: p.Environment}, false, nil
}
func TestPurchaseAPI(t *testing.T) {
	good := `{"customer_id":"cus-001","package_id":"pkg-10","payment_method":"Pulsa","environment":"development"}`
	tests := []struct {
		name, body, key, origin, content string
		want                             int
	}{
		{"business failure is a valid result", good, "test-idempotency-123", "", "application/json", 201},
		{"missing key", good, "", "", "application/json", 400},
		{"cross origin", good, "test-idempotency-123", "https://untrusted.example", "application/json", 403},
		{"unknown field", strings.TrimSuffix(good, "}") + `,"admin":true}`, "test-idempotency-123", "", "application/json", 400},
		{"trailing object", good + `{}`, "test-idempotency-123", "", "application/json", 400},
		{"invalid environment", strings.Replace(good, "development", "production", 1), "test-idempotency-123", "", "application/json", 422},
		{"invalid replay ID", strings.TrimSuffix(good, "}") + `,"replay_of":"other"}`, "test-idempotency-123", "", "application/json", 422},
		{"wrong content", good, "test-idempotency-123", "", "text/plain", 415},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeRepo{}
			s := Server{Repo: repo, Log: slog.New(slog.NewJSONHandler(io.Discard, nil)), Ready: func(context.Context) error { return nil }, Origin: "http://localhost:3000"}
			r := httptest.NewRequest(http.MethodPost, "/api/v1/transactions", strings.NewReader(tt.body))
			r.Header.Set("Idempotency-Key", tt.key)
			r.Header.Set("Origin", tt.origin)
			r.Header.Set("Content-Type", tt.content)
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, r)
			if w.Code != tt.want {
				t.Fatalf("status=%d body=%s", w.Code, w.Body)
			}
			if tt.want >= 400 && repo.calls != 0 {
				t.Fatal("invalid request reached repository")
			}
		})
	}
}

func TestInvalidReplayResponse(t *testing.T) {
	repo := &fakeRepo{err: workflow.ErrInvalidReplay}
	s := Server{Repo: repo, Log: slog.New(slog.NewJSONHandler(io.Discard, nil))}
	r := httptest.NewRequest(http.MethodPost, "/api/v1/transactions", strings.NewReader(`{"customer_id":"cus-001","package_id":"pkg-10","payment_method":"Pulsa","environment":"development","replay_of":"TXN-0123456789abcdef01234567"}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Idempotency-Key", "invalid-replay-source-key")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 422 || repo.calls != 1 || !strings.Contains(w.Body.String(), "matching failed transaction") {
		t.Fatalf("invalid replay response: %d %s", w.Code, w.Body.String())
	}
}

func TestPendingPurchaseAccepted(t *testing.T) {
	repo := &fakeRepo{pending: true}
	s := Server{Repo: repo, Log: slog.New(slog.NewJSONHandler(io.Discard, nil))}
	r := httptest.NewRequest(http.MethodPost, "/api/v1/transactions", strings.NewReader(`{"customer_id":"cus-001","package_id":"pkg-10","payment_method":"Pulsa","environment":"development"}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Idempotency-Key", "pending-purchase-key")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusAccepted || w.Header().Get("Location") != "/api/v1/transactions/TXN-pending" || !strings.Contains(w.Body.String(), "PROCESSING") {
		t.Fatalf("pending result %d %s", w.Code, w.Body)
	}
}

func TestPurchaseAdmission(t *testing.T) {
	for _, tt := range []struct {
		name   string
		retry  time.Duration
		err    error
		status int
	}{{"limited", 1200 * time.Millisecond, nil, 429}, {"unavailable", 0, errors.New("Redis down"), 503}, {"admitted", 0, nil, 201}} {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeRepo{}
			s := Server{Repo: repo, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), PurchaseBudget: func(context.Context) (time.Duration, error) { return tt.retry, tt.err }}
			r := httptest.NewRequest("POST", "/api/v1/transactions", strings.NewReader(`{"customer_id":"cus-001","package_id":"pkg-10","payment_method":"Pulsa","environment":"development"}`))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Idempotency-Key", "admission-test-key")
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, r)
			if w.Code != tt.status {
				t.Fatalf("status %d body %s", w.Code, w.Body.String())
			}
			if tt.status != 201 && repo.calls != 0 {
				t.Fatal("denied admission reached purchase")
			}
			if tt.status == 429 && w.Header().Get("Retry-After") != "2" {
				t.Fatal("incorrect retry interval")
			}
		})
	}
}

func TestTimeoutPreservesMatchedRoute(t *testing.T) {
	s := Server{Repo: &fakeRepo{}, Log: slog.New(slog.NewJSONHandler(io.Discard, nil))}
	r := httptest.NewRequest("POST", "/api/v1/transactions", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if r.Pattern != "POST /api/v1/transactions" {
		t.Fatalf("route lost through timeout middleware: %q", r.Pattern)
	}
	if w.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("unexpected status: %d", w.Code)
	}
}
