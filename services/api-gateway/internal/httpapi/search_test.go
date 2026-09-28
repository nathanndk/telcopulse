package httpapi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"telcopulse/services/api-gateway/internal/store"
)

type searchRepository struct {
	Repository
	environment string
	query       string
	err         error
	calls       int
}

func (r *searchRepository) WorkspaceSearch(_ context.Context, environment, query string) (store.WorkspaceResults, error) {
	r.calls++
	r.environment, r.query = environment, query
	return store.WorkspaceResults{Items: []store.WorkspaceHit{}}, r.err
}

func TestWorkspaceSearchRoute(t *testing.T) {
	for _, test := range []struct {
		url    string
		status int
		calls  int
	}{
		{"/api/v1/search?environment=staging&q=INC-123", 200, 1},
		{"/api/v1/search?environment=production&q=INC-123", 400, 0},
		{"/api/v1/search?environment=development&q=abc&extra=x", 400, 0},
	} {
		repo := &searchRepository{}
		s := Server{Repo: repo, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, httptest.NewRequest("GET", test.url, nil))
		if w.Code != test.status || repo.calls != test.calls {
			t.Fatalf("%s: status %d calls %d body %s", test.url, w.Code, repo.calls, w.Body.String())
		}
		if test.calls == 1 && (repo.environment != "staging" || repo.query != "INC-123") {
			t.Fatalf("lost search scope: %+v", repo)
		}
	}
	for _, test := range []struct {
		err    error
		status int
		body   string
	}{{store.ErrInvalidWorkspaceSearch, 422, "2 to 80"}, {errors.New("unavailable"), 503, "workspace search unavailable"}} {
		repo := &searchRepository{err: test.err}
		s := Server{Repo: repo, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/search?q=abc", nil))
		if w.Code != test.status || !strings.Contains(w.Body.String(), test.body) {
			t.Fatalf("status %d body %s", w.Code, w.Body.String())
		}
	}
}
