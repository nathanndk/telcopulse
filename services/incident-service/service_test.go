package incident

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"telcopulse/services/shared/runtime"
	"testing"
)

func TestIncidentIdentityRequiresActorAndRoleTogether(t *testing.T) {
	r := httptest.NewRequest("POST", "/internal/incidents", nil)
	r.Header.Set("X-Actor-ID", "administrator")
	if actor, role, ok := operatorIdentity(r); !ok || actor != "local-operator" || role != "" {
		t.Fatalf("browser identity became actor: %q %q %t", actor, role, ok)
	}
	r.Header.Set("X-Operator-Actor", "operator:USR-0123456789abcdef01234567")
	if _, _, ok := operatorIdentity(r); ok {
		t.Fatal("actor without role accepted")
	}
	r.Header.Set("X-Operator-Role", "Operator")
	if actor, role, ok := operatorIdentity(r); !ok || actor != "operator:USR-0123456789abcdef01234567" || role != "Operator" {
		t.Fatalf("verified identity lost: %q %q %t", actor, role, ok)
	}
	r.Header.Set("X-Operator-Actor", "administrator")
	if _, _, ok := operatorIdentity(r); ok {
		t.Fatal("malformed actor accepted")
	}
}

func TestEscalationRequiresCapabilityAndCommanderRole(t *testing.T) {
	mutationToken := "incident-mutation-test-token-123456"
	handler := Handler(nil, slog.New(slog.NewTextHandler(io.Discard, nil)), mutationToken)
	r := httptest.NewRequest(http.MethodPost, "/internal/incidents/INC-0123456789abcdef01234567/escalations", strings.NewReader(`{}`))
	r.Header.Set("X-Operator-Actor", "operator:USR-0123456789abcdef01234567")
	r.Header.Set("X-Operator-Role", "Operator")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("missing mutation capability = %d", w.Code)
	}
	r.Header.Set(runtime.MutationTokenHeader, mutationToken)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("Operator escalation = %d", w.Code)
	}
}

func TestPostmortemRequiresCapabilityAndCommanderRole(t *testing.T) {
	mutationToken := "incident-mutation-test-token-123456"
	handler := Handler(nil, slog.New(slog.NewTextHandler(io.Discard, nil)), mutationToken)
	r := httptest.NewRequest(http.MethodPost, "/internal/incidents/INC-0123456789abcdef01234567/postmortem", strings.NewReader(`{}`))
	r.Header.Set("X-Operator-Actor", "operator:USR-0123456789abcdef01234567")
	r.Header.Set("X-Operator-Role", "Operator")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("missing mutation capability = %d", w.Code)
	}
	r.Header.Set(runtime.MutationTokenHeader, mutationToken)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("Operator postmortem generation = %d", w.Code)
	}
}
