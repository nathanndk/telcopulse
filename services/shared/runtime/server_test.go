package runtime

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"telcopulse/services/shared/telemetry"
	"testing"
)

func TestProtectedRoutesRetainMetricPattern(t *testing.T) {
	m := telemetry.New("domain-test", nil)
	inner := http.NewServeMux()
	inner.HandleFunc("GET /customers/{id}", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := r.Context().Deadline(); !ok {
			t.Error("missing deadline")
		}
		w.WriteHeader(200)
	})
	outer := http.NewServeMux()
	outer.Handle("/", inner)
	h := m.Wrap(Protect(outer, "test-token"))
	r := httptest.NewRequest("GET", "/customers/private-id?secret=private-value", nil)
	r.Header.Set("Authorization", "Bearer test-token")
	h.ServeHTTP(httptest.NewRecorder(), r)
	w := httptest.NewRecorder()
	m.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/metrics", nil))
	body := w.Body.String()
	if !strings.Contains(body, `route="GET /customers/{id}"`) || strings.Contains(body, "private-") {
		t.Fatalf("wrong route labels: %s", body)
	}
}

func TestMutationCapabilityDoesNotReplaceSharedServiceAuthentication(t *testing.T) {
	const shared = "shared-service-token-for-test-123456"
	const mutation = "incident-operator-token-for-test-1234"
	handler := Protect(RequireMutationToken(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}), mutation, func(r *http.Request) bool { return r.Method == http.MethodPost }), shared)
	request := func(method, serviceToken, mutationToken string) int {
		r := httptest.NewRequest(method, "/internal/incidents", nil)
		r.Header.Set("Authorization", "Bearer "+serviceToken)
		r.Header.Set(MutationTokenHeader, mutationToken)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w.Code
	}
	if got := request("GET", shared, ""); got != 204 {
		t.Fatalf("shared read = %d", got)
	}
	if got := request("POST", shared, ""); got != 401 {
		t.Fatalf("shared mutation without capability = %d", got)
	}
	if got := request("POST", shared, mutation); got != 204 {
		t.Fatalf("gateway mutation = %d", got)
	}
	if got := request("POST", "wrong", mutation); got != 401 {
		t.Fatalf("mutation capability replaced shared authentication = %d", got)
	}
}
