package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"telcopulse/services/api-gateway/internal/store"
)

func (s *Server) workspaceSearch(w http.ResponseWriter, r *http.Request) {
	environment, ok := s.environment(w, r)
	if !ok {
		return
	}
	for key := range r.URL.Query() {
		if key != "environment" && key != "q" {
			s.write(w, 400, map[string]string{"error": "unknown search filter"})
			return
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	result, err := s.Repo.WorkspaceSearch(ctx, environment, r.URL.Query().Get("q"))
	if errors.Is(err, store.ErrInvalidWorkspaceSearch) {
		s.write(w, 422, map[string]string{"error": "search must contain 2 to 80 characters"})
		return
	}
	if err != nil {
		s.Log.Error("workspace search failed", "error", err)
		s.write(w, 503, map[string]string{"error": "workspace search unavailable"})
		return
	}
	s.write(w, 200, result)
}
