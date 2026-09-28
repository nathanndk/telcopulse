package httpapi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"telcopulse/services/shared/rpc"
)

type testAuth struct {
	user   Operator
	err    error
	manage func(context.Context, string, any) (rpc.Response, error)
}

func (a *testAuth) Login(_ context.Context, username, password string) (LoginSession, error) {
	if username != "operator" || password != "secret" {
		return LoginSession{}, errUnauthenticated
	}
	return LoginSession{Token: strings.Repeat("a", 64), ExpiresAt: time.Now().Add(time.Hour), User: a.user}, nil
}
func (a *testAuth) Validate(_ context.Context, token string) (Operator, error) {
	if a.err != nil {
		return Operator{}, a.err
	}
	if token != strings.Repeat("a", 64) {
		return Operator{}, errUnauthenticated
	}
	return a.user, nil
}
func (a *testAuth) Logout(_ context.Context, _ string) error { return a.err }
func (a *testAuth) Management(ctx context.Context, path string, input any) (rpc.Response, error) {
	if a.manage == nil {
		return rpc.Response{}, errors.New("management unavailable")
	}
	return a.manage(ctx, path, input)
}

func TestGatewayAuthenticationAndAuthorization(t *testing.T) {
	actor := ""
	role := ""
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		actor = r.Header.Get("X-Operator-Actor")
		role = r.Header.Get("X-Operator-Role")
		_, _ = io.WriteString(w, `{"id":"INC-0123456789abcdef01234567"}`)
	}))
	defer remote.Close()
	auth := &testAuth{user: Operator{ID: "USR-0123456789abcdef01234567", Username: "operator", Role: "Viewer"}}
	s := Server{Auth: auth, RequireAuth: true, Origin: "http://localhost:3000", Incidents: rpc.New(remote.URL, "internal-test-token"), Log: slog.New(slog.NewJSONHandler(io.Discard, nil))}
	send := func(method, path, origin, token string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(`{"title":"incident"}`))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Idempotency-Key", "incident-creation-key")
		r.Header.Set("X-Operator-Actor", "operator:USR-ffffffffffffffffffffffff")
		r.Header.Set("X-Operator-Role", "Incident Commander")
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		if token != "" {
			r.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
		}
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	if got := send("GET", "/api/v1/incidents", "", "").Code; got != 401 {
		t.Fatalf("anonymous read = %d", got)
	}
	if got := send("POST", "/api/v1/incidents", "", strings.Repeat("a", 64)).Code; got != 403 {
		t.Fatalf("missing origin = %d", got)
	}
	if got := send("POST", "/api/v1/incidents", s.Origin, strings.Repeat("a", 64)).Code; got != 403 {
		t.Fatalf("viewer mutation = %d", got)
	}
	auth.user.Role = "Administrator"
	if got := send("POST", "/api/v1/incidents", s.Origin, strings.Repeat("a", 64)).Code; got != 200 {
		t.Fatalf("admin mutation = %d", got)
	}
	if actor != "operator:"+auth.user.ID || role != "Administrator" {
		t.Fatalf("identity was not derived from validated session: %q %q", actor, role)
	}
	auth.err = errors.New("backend unavailable")
	if got := send("GET", "/api/v1/incidents", "", strings.Repeat("a", 64)).Code; got != 503 {
		t.Fatalf("authentication outage = %d", got)
	}
	if got := send("GET", "/api/v1/incidents", "", "invalid").Code; got != 503 {
		t.Fatalf("outage with invalid token = %d", got)
	}
}

func TestEscalationRolePolicy(t *testing.T) {
	path := "/api/v1/incidents/INC-0123456789abcdef01234567/escalations"
	for _, role := range []string{"Viewer", "Operator", "Engineer"} {
		if allowedMutation(role, http.MethodPost, path) {
			t.Errorf("%s can escalate", role)
		}
	}
	for _, role := range []string{"Incident Commander", "Administrator"} {
		if !allowedMutation(role, http.MethodPost, path) {
			t.Errorf("%s cannot escalate", role)
		}
	}
}

func TestSavedViewRolePolicy(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		if !allowedMutation("Viewer", method, "/api/v1/incidents/saved-views/VIEW-0123456789abcdef01234567") {
			t.Fatalf("viewer cannot manage own view with %s", method)
		}
	}
	if allowedMutation("Viewer", http.MethodPut, "/api/v1/incidents/INC-0123456789abcdef01234567") {
		t.Fatal("view permission granted incident editing")
	}
}

func TestGatewayLoginCookieAndLogout(t *testing.T) {
	auth := &testAuth{user: Operator{ID: "USR-0123456789abcdef01234567", Username: "operator", Role: "Operator"}}
	s := Server{Auth: auth, RequireAuth: true, Origin: "http://localhost:3000"}
	status := httptest.NewRecorder()
	s.Handler().ServeHTTP(status, httptest.NewRequest("GET", "/api/v1/auth/status", nil))
	if status.Code != 200 || !strings.Contains(status.Body.String(), `"required":true`) {
		t.Fatalf("public auth status missing: %d %s", status.Code, status.Body.String())
	}
	request := func(method, path, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Origin", s.Origin)
		r.Header.Set("Content-Type", "application/json")
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	if got := request("POST", "/api/v1/auth/login", `{"username":"operator","password":"wrong"}`, nil).Code; got != 401 {
		t.Fatalf("bad credentials = %d", got)
	}
	w := request("POST", "/api/v1/auth/login", `{"username":"operator","password":"secret"}`, nil)
	if w.Code != 200 || strings.Contains(w.Body.String(), strings.Repeat("a", 64)) {
		t.Fatalf("login leaked token or failed: %d %s", w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode || cookies[0].MaxAge < 1 {
		t.Fatalf("insecure session cookie: %+v", cookies)
	}
	if got := request("GET", "/api/v1/auth/me", "", cookies[0]).Code; got != 200 {
		t.Fatalf("me = %d", got)
	}
	w = request("POST", "/api/v1/auth/logout", "", cookies[0])
	if w.Code != 200 || len(w.Result().Cookies()) != 1 || w.Result().Cookies()[0].MaxAge >= 0 {
		t.Fatalf("logout did not clear cookie: %d", w.Code)
	}
}

func TestRolePolicy(t *testing.T) {
	cases := []struct {
		role, method, path string
		allowed            bool
	}{
		{"Viewer", "GET", "/api/v1/incidents", true},
		{"Viewer", "POST", "/api/v1/incidents", false},
		{"Viewer", "POST", "/api/v1/simulations", false},
		{"Operator", "POST", "/api/v1/incidents", true},
		{"Operator", "PUT", "/api/v1/incidents/INC-0123456789abcdef01234567", true},
		{"Operator", "POST", "/api/v1/simulations", false},
		{"Engineer", "POST", "/api/v1/simulations", true},
		{"Engineer", "POST", "/api/v1/incidents", false},
		{"Incident Commander", "PUT", "/api/v1/incidents/INC-0123456789abcdef01234567", true},
		{"Administrator", "POST", "/api/v1/simulations", true},
		{"Administrator", "DELETE", "/api/v1/incidents/INC-0123456789abcdef01234567", false},
	}
	for _, tc := range cases {
		if got := allowedMutation(tc.role, tc.method, tc.path); got != tc.allowed {
			t.Errorf("%s %s %s: got %t want %t", tc.role, tc.method, tc.path, got, tc.allowed)
		}
	}
}
