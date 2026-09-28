package httpapi

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"telcopulse/services/shared/rpc"
)

func TestDeploymentGatewayReadBoundary(t *testing.T) {
	calls := 0
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "GET" || r.Header.Get("Authorization") != "Bearer service-token" || r.Header.Get("Cookie") != "" || r.Header.Get("X-Actor-ID") != "" {
			t.Error("browser identity crossed service boundary")
		}
		if r.URL.Path != "/internal/deployments" || r.URL.Query().Get("environment") != "staging" || r.URL.Query().Get("service") != "payment-service" || r.URL.Query().Has("unauthorized") {
			t.Errorf("unexpected deployment query: %s", r.URL)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":"jenkins-deploy-0001","status":"Completed"}]`))
	}))
	defer remote.Close()
	s := Server{Deployments: rpc.New(remote.URL, "service-token"), Log: slog.New(slog.NewJSONHandler(io.Discard, nil))}
	r := httptest.NewRequest("GET", "/api/v1/deployments?environment=staging&service=payment-service&unauthorized=secret", nil)
	r.Header.Set("Cookie", "session=browser-secret")
	r.Header.Set("X-Actor-ID", "administrator")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 200 || calls != 1 || !strings.Contains(w.Body.String(), "jenkins-deploy-0001") {
		t.Fatalf("deployment response lost: %d %s", w.Code, w.Body)
	}
	bad := httptest.NewRecorder()
	s.Handler().ServeHTTP(bad, httptest.NewRequest("GET", "/api/v1/deployments/bad!", nil))
	if bad.Code != 404 || calls != 1 {
		t.Fatalf("invalid id forwarded: %d", bad.Code)
	}
}
