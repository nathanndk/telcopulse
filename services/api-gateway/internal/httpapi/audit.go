package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"telcopulse/services/api-gateway/internal/store"
)

func (s *Server) auditEvents(w http.ResponseWriter, r *http.Request) {
	environment, ok := s.environment(w, r)
	if !ok {
		return
	}
	for key := range r.URL.Query() {
		switch key {
		case "environment", "source", "actor", "action", "resource", "cursor", "limit":
		default:
			s.write(w, 400, map[string]string{"error": "unknown audit filter"})
			return
		}
	}
	limit := 25
	if raw := r.URL.Query().Get("limit"); raw != "" {
		var err error
		limit, err = strconv.Atoi(raw)
		if err != nil {
			s.write(w, 400, map[string]string{"error": "invalid audit page size"})
			return
		}
	}
	filter := store.AuditFilter{Environment: environment, Source: r.URL.Query().Get("source"),
		Actor: r.URL.Query().Get("actor"), Action: r.URL.Query().Get("action"),
		Resource: r.URL.Query().Get("resource"), Cursor: r.URL.Query().Get("cursor"), Limit: limit}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	result, err := s.Repo.AuditEvents(ctx, filter)
	if errors.Is(err, store.ErrInvalidAuditFilter) {
		s.write(w, 422, map[string]string{"error": "invalid audit filter or cursor"})
		return
	}
	if err != nil {
		s.Log.Error("load operations audit", "error", err)
		s.write(w, 503, map[string]string{"error": "operations audit unavailable"})
		return
	}
	s.write(w, 200, result)
}
