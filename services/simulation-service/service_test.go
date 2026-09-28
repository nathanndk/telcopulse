package simulation

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"telcopulse/services/shared/runtime"
	"testing"
)

func TestSimulationIdentity(t *testing.T) {
	for _, tc := range []struct {
		actor, role, wantActor string
		allowed                bool
	}{
		{"", "", "local-operator", true},
		{"operator:USR-0123456789abcdef01234567", "Engineer", "operator:USR-0123456789abcdef01234567", true},
		{"operator:USR-0123456789abcdef01234567", "Administrator", "operator:USR-0123456789abcdef01234567", true},
		{"operator:USR-0123456789abcdef01234567", "Operator", "operator:USR-0123456789abcdef01234567", false},
		{"operator:USR-0123456789abcdef01234567", "", "operator:USR-0123456789abcdef01234567", false},
		{"", "Engineer", "", false},
		{"operator:USR-../../invalid", "Engineer", "operator:USR-../../invalid", false},
	} {
		r := httptest.NewRequest(http.MethodPost, "/internal/simulations", nil)
		r.Header.Set("X-Operator-Actor", tc.actor)
		r.Header.Set("X-Operator-Role", tc.role)
		actor, allowed := simulationIdentity(r)
		if actor != tc.wantActor || allowed != tc.allowed {
			t.Errorf("identity(%q, %q) = %q, %v", tc.actor, tc.role, actor, allowed)
		}
	}
}

func TestUnauthorizedSimulationMutationStopsBeforeStore(t *testing.T) {
	mutationToken := "simulation-mutation-test-token-123456"
	handler := Handler(nil, slog.New(slog.NewTextHandler(io.Discard, nil)), mutationToken)
	for _, path := range []string{"/internal/simulations", "/internal/simulations/SIM-0123456789abcdef01234567/stop"} {
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
		r.Header.Set("X-Operator-Actor", "operator:USR-0123456789abcdef01234567")
		r.Header.Set("X-Operator-Role", "Operator")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s without mutation token status = %d", path, w.Code)
		}
		r.Header.Set(runtime.MutationTokenHeader, mutationToken)
		w = httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != http.StatusForbidden {
			t.Errorf("%s status = %d", path, w.Code)
		}
	}
}
