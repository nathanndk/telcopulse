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

type auditRepository struct {
	Repository
	filter store.AuditFilter
	err    error
	calls  int
}

func (r *auditRepository) AuditEvents(_ context.Context, filter store.AuditFilter) (store.AuditPage, error) {
	r.calls++
	r.filter = filter
	return store.AuditPage{Items: []store.AuditEvent{}}, r.err
}

func TestAuditRouteValidationAndScope(t *testing.T) {
	for _, tc := range []struct {
		url    string
		status int
		calls  int
	}{
		{"/api/v1/audit?environment=staging&source=incident&actor=alice&action=resolved&resource=INC-1&limit=4", 200, 1},
		{"/api/v1/audit?environment=production", 400, 0},
		{"/api/v1/audit?unknown=value", 400, 0},
		{"/api/v1/audit?limit=oops", 400, 0},
	} {
		repo := &auditRepository{}
		s := Server{Repo: repo, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, httptest.NewRequest("GET", tc.url, nil))
		if w.Code != tc.status || repo.calls != tc.calls {
			t.Fatalf("%s: status %d calls %d body %s", tc.url, w.Code, repo.calls, w.Body.String())
		}
		if tc.calls == 1 && (repo.filter.Environment != "staging" || repo.filter.Resource != "INC-1" || repo.filter.Limit != 4 || repo.filter.Action != "resolved") {
			t.Fatalf("lost audit filter: %+v", repo.filter)
		}
	}
	for _, tc := range []struct {
		err    error
		status int
		body   string
	}{{store.ErrInvalidAuditFilter, 422, "invalid audit filter"}, {errors.New("database offline"), 503, "operations audit unavailable"}} {
		repo := &auditRepository{err: tc.err}
		s := Server{Repo: repo, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/audit", nil))
		if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.body) {
			t.Fatalf("status %d body %s", w.Code, w.Body.String())
		}
	}
}
