package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"telcopulse/services/shared/rpc"
	"telcopulse/services/shared/runtime"
)

type userManager interface {
	Management(context.Context, string, any) (rpc.Response, error)
}

func (s *Server) userRoutes(mux *http.ServeMux) {
	for _, pattern := range []string{
		"GET /api/v1/auth/users",
		"POST /api/v1/auth/users",
		"PATCH /api/v1/auth/users/{id}",
	} {
		mux.HandleFunc(pattern, s.usersRequest)
	}
}

func (s *Server) usersRequest(w http.ResponseWriter, r *http.Request) {
	manager, ok := s.Auth.(userManager)
	if !s.RequireAuth || !ok {
		s.write(w, 503, map[string]string{"error": "user management is not enabled"})
		return
	}
	if operatorFrom(r.Context()).Role != "Administrator" {
		s.write(w, 403, map[string]string{"error": "administrator role required"})
		return
	}
	cookie, err := r.Cookie(sessionCookie)
	if err != nil || cookie.Value == "" {
		s.write(w, 401, map[string]string{"error": "authentication required"})
		return
	}
	path := "/internal/auth/users/list"
	input := map[string]any{"token": cookie.Value}
	if r.Method == http.MethodGet {
		cursor := r.URL.Query().Get("cursor")
		if len(cursor) > 512 {
			s.write(w, 400, map[string]string{"error": "invalid user cursor"})
			return
		}
		input["cursor"] = cursor
	} else {
		if strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0]) != "application/json" {
			s.write(w, 415, map[string]string{"error": "use application/json"})
			return
		}
		var body json.RawMessage
		if !runtime.DecodeLimit(w, r, &body, 4096) {
			return
		}
		if r.Method == http.MethodPost {
			path = "/internal/auth/users/create"
			input["user"] = body
		} else {
			id := r.PathValue("id")
			if !operatorID.MatchString(id) {
				s.write(w, 404, map[string]string{"error": "user not found"})
				return
			}
			path = "/internal/auth/users/" + id + "/update"
			input["change"] = body
		}
	}
	result, err := manager.Management(r.Context(), path, input)
	if err != nil {
		s.Log.Warn("user management request failed", "error", err)
		s.write(w, 503, map[string]string{"error": "user management unavailable"})
		return
	}
	switch result.Status {
	case 200, 201, 400, 401, 403, 404, 409, 422:
		s.write(w, result.Status, result.Body)
	default:
		s.write(w, 503, map[string]string{"error": "user management unavailable"})
	}
}
