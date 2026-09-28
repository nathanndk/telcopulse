package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"telcopulse/services/shared/rpc"
)

func TestAdministratorUserRoutesUseValidatedSession(t *testing.T) {
	var paths []string
	auth := &testAuth{user: Operator{ID: "USR-0123456789abcdef01234567", Username: "admin", Role: "Viewer"}}
	auth.manage = func(_ context.Context, path string, input any) (rpc.Response, error) {
		paths = append(paths, path)
		data, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		var envelope map[string]json.RawMessage
		if err = json.Unmarshal(data, &envelope); err != nil {
			t.Fatal(err)
		}
		var token string
		if err = json.Unmarshal(envelope["token"], &token); err != nil || token != strings.Repeat("a", 64) {
			t.Fatalf("management token not taken from session cookie")
		}
		return rpc.Response{Status: 200, Body: json.RawMessage(`{"items":[]}`)}, nil
	}
	s := &Server{Auth: auth, RequireAuth: true, Origin: "http://localhost:3000", Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	send := func(method, path, body string) int {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Origin", s.Origin)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Operator-Role", "Administrator")
		r.AddCookie(&http.Cookie{Name: sessionCookie, Value: strings.Repeat("a", 64)})
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w.Code
	}
	if got := send("GET", "/api/v1/auth/users", ""); got != 403 || len(paths) != 0 {
		t.Fatalf("Viewer list allowed: %d, calls=%d", got, len(paths))
	}
	if got := send("POST", "/api/v1/auth/users", `{}`); got != 403 || len(paths) != 0 {
		t.Fatalf("Viewer create allowed: %d, calls=%d", got, len(paths))
	}
	auth.user.Role = "Administrator"
	if got := send("GET", "/api/v1/auth/users?cursor=test", ""); got != 200 {
		t.Fatalf("Admin list = %d", got)
	}
	if got := send("POST", "/api/v1/auth/users", `{"username":"triage","password":"not-a-real-secret-123","role":"Viewer"}`); got != 200 {
		t.Fatalf("Admin create = %d", got)
	}
	if got := send("PATCH", "/api/v1/auth/users/USR-aaaaaaaaaaaaaaaaaaaaaaaa", `{"role":"Operator"}`); got != 200 {
		t.Fatalf("Admin update = %d", got)
	}
	if got := send("PATCH", "/api/v1/auth/users/invalid", `{}`); got != 404 {
		t.Fatalf("Invalid target = %d", got)
	}
	want := []string{"/internal/auth/users/list", "/internal/auth/users/create", "/internal/auth/users/USR-aaaaaaaaaaaaaaaaaaaaaaaa/update"}
	if len(paths) != len(want) {
		t.Fatalf("management calls = %v", paths)
	}
	for i := range want {
		if paths[i] != want[i] {
			t.Fatalf("management paths = %v", paths)
		}
	}
}
