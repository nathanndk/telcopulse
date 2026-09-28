package auth

import (
	"errors"
	"log/slog"
	"net/http"

	"telcopulse/services/shared/runtime"
)

func managementRoutes(mux *http.ServeMux, store Store, log *slog.Logger) {
	respond := func(w http.ResponseWriter, value any, err error, success int) {
		switch {
		case err == nil:
			runtime.JSON(w, success, value)
		case errors.Is(err, ErrUnauthorized):
			runtime.JSON(w, 401, map[string]string{"error": "invalid session"})
		case errors.Is(err, ErrForbidden):
			runtime.JSON(w, 403, map[string]string{"error": "administrator role required"})
		case errors.Is(err, ErrInvalid):
			runtime.JSON(w, 422, map[string]string{"error": "invalid user management request"})
		case errors.Is(err, ErrConflict):
			runtime.JSON(w, 409, map[string]string{"error": "username already exists or last active administrator cannot be removed"})
		case errors.Is(err, ErrNotFound):
			runtime.JSON(w, 404, map[string]string{"error": "user not found"})
		default:
			log.Error("user management unavailable", "error", err)
			runtime.JSON(w, 503, map[string]string{"error": "user management unavailable"})
		}
	}
	mux.HandleFunc("POST /internal/auth/users/list", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Token  string `json:"token"`
			Cursor string `json:"cursor"`
		}
		if !runtime.Decode(w, r, &input) {
			return
		}
		value, err := store.ListUsers(r.Context(), input.Token, input.Cursor)
		respond(w, value, err, 200)
	})
	mux.HandleFunc("POST /internal/auth/users/create", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Token string     `json:"token"`
			User  CreateUser `json:"user"`
		}
		if !runtime.Decode(w, r, &input) {
			return
		}
		value, err := store.CreateUser(r.Context(), input.Token, input.User)
		respond(w, value, err, 201)
	})
	mux.HandleFunc("POST /internal/auth/users/{id}/update", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Token  string     `json:"token"`
			Change UpdateUser `json:"change"`
		}
		if !runtime.Decode(w, r, &input) {
			return
		}
		value, err := store.UpdateUser(r.Context(), input.Token, r.PathValue("id"), input.Change)
		respond(w, value, err, 200)
	})
}
