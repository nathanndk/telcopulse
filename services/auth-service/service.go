package auth

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
	"telcopulse/services/shared/runtime"
)

// Handler exposes only service-authenticated internal authentication calls.
func Handler(pool *pgxpool.Pool, log *slog.Logger, dummy []byte) http.Handler {
	store := Store{Pool: pool, DummyHash: dummy}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /internal/auth/login", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if !runtime.Decode(w, r, &input) {
			return
		}
		session, err := store.Login(r.Context(), input.Username, input.Password)
		switch {
		case err == nil:
			log.Info("operator login succeeded", "user_id", session.User.ID)
			runtime.JSON(w, 200, session)
		case errors.Is(err, ErrInvalid), errors.Is(err, ErrUnauthorized):
			log.Warn("operator login rejected")
			runtime.JSON(w, 401, map[string]string{"error": "invalid credentials"})
		default:
			log.Error("operator login unavailable", "error", err)
			runtime.JSON(w, 503, map[string]string{"error": "authentication unavailable"})
		}
	})
	mux.HandleFunc("POST /internal/auth/validate", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Token string `json:"token"`
		}
		if !runtime.Decode(w, r, &input) {
			return
		}
		user, err := store.Validate(r.Context(), input.Token)
		switch {
		case err == nil:
			runtime.JSON(w, 200, user)
		case errors.Is(err, ErrUnauthorized):
			runtime.JSON(w, 401, map[string]string{"error": "invalid session"})
		default:
			log.Error("operator session validation unavailable", "error", err)
			runtime.JSON(w, 503, map[string]string{"error": "authentication unavailable"})
		}
	})
	mux.HandleFunc("POST /internal/auth/logout", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Token string `json:"token"`
		}
		if !runtime.Decode(w, r, &input) {
			return
		}
		err := store.Logout(r.Context(), input.Token)
		if err != nil && !errors.Is(err, ErrUnauthorized) {
			log.Error("operator logout unavailable", "error", err)
			runtime.JSON(w, 503, map[string]string{"error": "authentication unavailable"})
			return
		}
		runtime.JSON(w, 200, map[string]string{"status": "revoked"})
	})
	managementRoutes(mux, store, log)
	return mux
}
