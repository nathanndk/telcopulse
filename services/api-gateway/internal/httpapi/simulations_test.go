package httpapi

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"telcopulse/services/shared/rpc"
	"testing"
)

func TestSimulationBoundary(t *testing.T) {
	var hits int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.Header.Get("Authorization") != "Bearer internal-test-token" || r.Header.Get("Cookie") != "" {
			t.Error("browser credentials crossed boundary")
		}
		if (r.URL.Path != "/internal/simulations" && r.URL.Path != "/internal/simulations/SIM-aaaaaaaaaaaaaaaaaaaaaaaa") || r.URL.Query().Get("url") != "" {
			t.Error("unapproved destination")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer upstream.Close()
	s := &Server{Simulations: rpc.New(upstream.URL, "internal-test-token"), Origin: "http://localhost:3001", Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	handler := s.Handler()
	for _, tc := range []struct {
		name, method, path, body, origin, key string
		status                                int
	}{
		{"list", "GET", "/api/v1/simulations?environment=development&url=http://evil", "", "", "", 200},
		{"detail", "GET", "/api/v1/simulations/SIM-aaaaaaaaaaaaaaaaaaaaaaaa?cursor=abc&url=http://evil", "", "", "", 200},
		{"bad env", "GET", "/api/v1/simulations?environment=production", "", "", "", 400},
		{"origin", "POST", "/api/v1/simulations", "{}", "http://evil", "valid-key-1234567", 403},
		{"missing key", "POST", "/api/v1/simulations", "{}", "", "", 400},
		{"bad id", "POST", "/api/v1/simulations/not-a-run/stop", "{}", "", "", 404},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Authorization", "Bearer browser-secret")
			r.Header.Set("Cookie", "secret=browser")
			r.Header.Set("Origin", tc.origin)
			r.Header.Set("Idempotency-Key", tc.key)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("status %d: %s", w.Code, w.Body.String())
			}
		})
	}
	if hits != 2 {
		t.Fatalf("unexpected upstream calls %d", hits)
	}
}

func TestSimulationActorComesFromValidatedSession(t *testing.T) {
	var identities []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identities = append(identities, r.Header.Get("X-Operator-Actor")+"|"+r.Header.Get("X-Operator-Role"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer upstream.Close()
	auth := &testAuth{user: Operator{ID: "USR-0123456789abcdef01234567", Username: "engineer", Role: "Operator"}}
	s := &Server{Auth: auth, RequireAuth: true, Simulations: rpc.New(upstream.URL, "internal-test-token"), Origin: "http://localhost:3001", Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	send := func(path string) int {
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"reason":"test"}`))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", s.Origin)
		r.Header.Set("Idempotency-Key", "simulation-key-123456")
		r.Header.Set("X-Operator-Actor", "operator:USR-ffffffffffffffffffffffff")
		r.Header.Set("X-Operator-Role", "Administrator")
		r.AddCookie(&http.Cookie{Name: sessionCookie, Value: strings.Repeat("a", 64)})
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w.Code
	}
	if got := send("/api/v1/simulations"); got != 403 || len(identities) != 0 {
		t.Fatalf("Operator injection status=%d forwarded=%d", got, len(identities))
	}
	auth.user.Role = "Engineer"
	if got := send("/api/v1/simulations"); got != 200 {
		t.Fatalf("Engineer injection status=%d", got)
	}
	auth.user.Role = "Administrator"
	if got := send("/api/v1/simulations/SIM-0123456789abcdef01234567/stop"); got != 200 {
		t.Fatalf("Administrator stop status=%d", got)
	}
	if len(identities) != 2 || identities[0] != "operator:"+auth.user.ID+"|Engineer" || identities[1] != "operator:"+auth.user.ID+"|Administrator" {
		t.Fatalf("browser headers crossed boundary: %v", identities)
	}
}
