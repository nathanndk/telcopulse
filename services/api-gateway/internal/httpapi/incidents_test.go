package httpapi

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"telcopulse/services/shared/rpc"
	"testing"
)

func TestIncidentGatewayBoundary(t *testing.T) {
	calls := 0
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer internal-test-token" {
			t.Error("missing service credential")
		}
		for _, name := range []string{"Cookie", "Origin", "X-Actor-ID"} {
			if r.Header.Get(name) != "" {
				t.Errorf("browser header forwarded: %s", name)
			}
		}
		if r.Header.Get("Idempotency-Key") != "incident-creation-key" {
			t.Error("creation key lost")
		}
		if r.URL.Path != "/internal/incidents" {
			t.Errorf("unexpected internal path %s", r.URL.Path)
		}
		var input map[string]any
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Error(err)
		}
		if input["title"] != "Payment unavailable" {
			t.Error("request body changed")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		if _, err := w.Write([]byte(`{"id":"INC-0123456789abcdef01234567","state":"Detected"}`)); err != nil {
			t.Error(err)
		}
	}))
	defer remote.Close()
	server := Server{Incidents: rpc.New(remote.URL, "internal-test-token"), Origin: "http://localhost:3000", Log: slog.New(slog.NewJSONHandler(io.Discard, nil))}
	send := func(origin, key string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/incidents", strings.NewReader(`{"title":"Payment unavailable"}`))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", origin)
		r.Header.Set("Idempotency-Key", key)
		r.Header.Set("Authorization", "Bearer browser-secret")
		r.Header.Set("Cookie", "session=browser-secret")
		r.Header.Set("X-Actor-ID", "administrator")
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, r)
		return w
	}
	if w := send("https://untrusted.example", "incident-creation-key"); w.Code != 403 {
		t.Fatal("cross-origin mutation allowed")
	}
	if w := send("http://localhost:3000", ""); w.Code != 400 {
		t.Fatal("missing creation key allowed")
	}
	if calls != 0 {
		t.Fatal("invalid request reached internal service")
	}
	w := send("http://localhost:3000", "incident-creation-key")
	if w.Code != 201 || !strings.Contains(w.Body.String(), "INC-0123456789abcdef01234567") || calls != 1 {
		t.Fatalf("resource response lost: %d %s", w.Code, w.Body)
	}
}

func TestIncidentLogSearchGatewayForwardsOnlyWindow(t *testing.T) {
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/internal/incidents/INC-0123456789abcdef01234567/logs" || r.URL.Query().Get("window") != "2h" || r.URL.Query().Get("search") != "" {
			t.Errorf("unsafe log query forwarding: %s", r.URL.String())
		}
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "Bearer internal-token" {
			t.Error("browser credentials reached log search")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"configured":false,"source":"splunk","items":[]}`)
	}))
	defer remote.Close()
	s := Server{Incidents: rpc.New(remote.URL, "internal-token"), Log: slog.New(slog.NewJSONHandler(io.Discard, nil))}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/incidents/INC-0123456789abcdef01234567/logs?window=2h&search=unsafe", nil)
	req.Header.Set("Cookie", "session=browser-secret")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"configured":false`) {
		t.Fatalf("log response: %d %s", w.Code, w.Body.String())
	}
}

func TestIncidentInfrastructureGatewayForwardsOnlyWindow(t *testing.T) {
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/internal/incidents/INC-0123456789abcdef01234567/infrastructure" || r.URL.Query().Get("window") != "2h" || r.URL.Query().Get("query") != "" {
			t.Errorf("unsafe infrastructure query forwarding: %s", r.URL.String())
		}
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "Bearer internal-token" {
			t.Error("browser credentials reached infrastructure search")
		}
		_, _ = io.WriteString(w, `{"configured":false,"source":"datadog","limited":false,"series":[]}`)
	}))
	defer remote.Close()
	s := Server{Incidents: rpc.New(remote.URL, "internal-token"), Log: slog.New(slog.NewJSONHandler(io.Discard, nil))}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/incidents/INC-0123456789abcdef01234567/infrastructure?window=2h&query=unsafe", nil)
	req.Header.Set("Cookie", "session=browser-secret")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"configured":false`) {
		t.Fatalf("infrastructure response: %d %s", w.Code, w.Body.String())
	}
}
func TestIncidentRecoveryGatewayForwardsNoClientQuery(t *testing.T) {
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/internal/incidents/INC-0123456789abcdef01234567/recovery-assessment" || r.URL.RawQuery != "" {
			t.Errorf("unsafe recovery query forwarding: %s", r.URL.String())
		}
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "Bearer internal-token" {
			t.Error("browser credentials reached incident service")
		}
		_, _ = io.WriteString(w, `{"applicable":true,"status":"collecting"}`)
	}))
	defer remote.Close()
	s := Server{Incidents: rpc.New(remote.URL, "internal-token"), Log: slog.New(slog.NewJSONHandler(io.Discard, nil))}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/incidents/INC-0123456789abcdef01234567/recovery-assessment?environment=staging&limit=999", nil)
	req.Header.Set("Cookie", "session=browser-secret")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"status":"collecting"`) {
		t.Fatalf("recovery response: %d %s", w.Code, w.Body.String())
	}
}
func TestIncidentTraceSearchGatewayForwardsOnlyWindow(t *testing.T) {
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/internal/incidents/INC-0123456789abcdef01234567/traces" || r.URL.Query().Get("window") != "2h" || r.URL.Query().Get("search") != "" {
			t.Errorf("unsafe trace query forwarding: %s", r.URL.String())
		}
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "Bearer internal-token" {
			t.Error("browser credentials reached trace search")
		}
		_, _ = io.WriteString(w, `{"configured":true,"source":"jaeger","items":[]}`)
	}))
	defer remote.Close()
	s := Server{Incidents: rpc.New(remote.URL, "internal-token"), Log: slog.New(slog.NewJSONHandler(io.Discard, nil))}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/incidents/INC-0123456789abcdef01234567/traces?window=2h&search=unsafe", nil)
	req.Header.Set("Cookie", "session=browser-secret")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"source":"jaeger"`) {
		t.Fatalf("trace response: %d %s", w.Code, w.Body.String())
	}
}
func TestIncidentMetricSearchGatewayForwardsOnlyWindow(t *testing.T) {
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/internal/incidents/INC-0123456789abcdef01234567/metrics" || r.URL.Query().Get("window") != "2h" || r.URL.Query().Get("query") != "" {
			t.Errorf("unsafe metric query forwarding: %s", r.URL.String())
		}
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "Bearer internal-token" {
			t.Error("browser credentials reached metric query")
		}
		_, _ = io.WriteString(w, `{"configured":true,"source":"prometheus","series":[]}`)
	}))
	defer remote.Close()
	s := Server{Incidents: rpc.New(remote.URL, "internal-token"), Log: slog.New(slog.NewJSONHandler(io.Discard, nil))}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/incidents/INC-0123456789abcdef01234567/metrics?window=2h&query=unsafe", nil)
	req.Header.Set("Cookie", "session=browser-secret")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"source":"prometheus"`) {
		t.Fatalf("metric response: %d %s", w.Code, w.Body.String())
	}
}
func TestIncidentPurchaseImpactGatewayForwardsOnlyWindowAndCursor(t *testing.T) {
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/internal/incidents/INC-0123456789abcdef01234567/purchase-impact" || r.URL.Query().Get("window") != "2h" || r.URL.Query().Get("cursor") != "safe-cursor" || r.URL.Query().Get("customer_id") != "" {
			t.Errorf("unsafe impact query forwarding: %s", r.URL.String())
		}
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "Bearer internal-token" {
			t.Error("browser credentials reached purchase impact")
		}
		_, _ = io.WriteString(w, `{"applicable":true,"scope":"purchase_path_environment","items":[]}`)
	}))
	defer remote.Close()
	s := Server{Incidents: rpc.New(remote.URL, "internal-token"), Log: slog.New(slog.NewJSONHandler(io.Discard, nil))}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/incidents/INC-0123456789abcdef01234567/purchase-impact?window=2h&cursor=safe-cursor&customer_id=unsafe", nil)
	req.Header.Set("Cookie", "session=browser-secret")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"scope":"purchase_path_environment"`) {
		t.Fatalf("impact response: %d %s", w.Code, w.Body.String())
	}
}
func TestIncidentGatewayStatuses(t *testing.T) {
	for _, status := range []int{200, 201, 400, 403, 404, 409, 422, 401, 503} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				if _, err := w.Write([]byte(`{"error":"upstream-detail"}`)); err != nil {
					t.Error(err)
				}
			}))
			defer remote.Close()
			s := Server{Incidents: rpc.New(remote.URL, "internal-token"), Log: slog.New(slog.NewJSONHandler(io.Discard, nil))}
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/incidents?environment=development", nil))
			want := status
			if status == 401 || status == 503 {
				want = 503
				if strings.Contains(w.Body.String(), "upstream-detail") {
					t.Fatal("upstream failure leaked")
				}
			}
			if w.Code != want {
				t.Fatalf("got %d want %d", w.Code, want)
			}
		})
	}
}
