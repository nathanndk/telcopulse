package simulation

import (
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"telcopulse/services/shared/faults"
	rt "telcopulse/services/shared/runtime"
)

var transactionID = regexp.MustCompile(`^[A-Za-z0-9_-]{16,100}$`)
var runID = regexp.MustCompile(`^SIM-[a-f0-9]{24}$`)
var verifiedActor = regexp.MustCompile(`^operator:USR-[a-f0-9]{24}$`)

func simulationIdentity(r *http.Request) (string, bool) {
	actor := r.Header.Get("X-Operator-Actor")
	role := r.Header.Get("X-Operator-Role")
	if actor == "" && role == "" {
		return "local-operator", true
	}
	return actor, verifiedActor.MatchString(actor) && (role == "Engineer" || role == "Administrator")
}

// Handler exposes commands only behind runtime's internal authentication boundary.
func Handler(pool *pgxpool.Pool, log *slog.Logger, mutationToken string) http.Handler {
	s := Store{Pool: pool}
	mux := http.NewServeMux()
	respond := func(w http.ResponseWriter, v any, err error, status int) {
		if err == nil {
			rt.JSON(w, status, v)
			return
		}
		code := 503
		message := "simulation service unavailable"
		switch {
		case errors.Is(err, ErrForbidden):
			code = 403
			message = "role cannot perform this action"
		case errors.Is(err, ErrInvalid):
			code = 422
			message = "invalid simulation command"
		case errors.Is(err, ErrConflict):
			code = 409
			message = "simulation command conflicts with existing state"
		case errors.Is(err, pgx.ErrNoRows):
			code = 404
			message = "simulation not found"
		default:
			log.Error("simulation request failed", "error", err)
		}
		rt.JSON(w, code, map[string]string{"error": message})
	}
	mux.HandleFunc("GET /internal/simulations", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.List(r.Context(), r.URL.Query().Get("environment"))
		respond(w, v, e, 200)
	})
	mux.HandleFunc("GET /internal/simulations/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !runID.MatchString(r.PathValue("id")) {
			respond(w, nil, pgx.ErrNoRows, 404)
			return
		}
		v, e := s.Get(r.Context(), r.PathValue("id"), r.URL.Query().Get("cursor"))
		respond(w, v, e, 200)
	})
	mux.HandleFunc("POST /internal/simulations", func(w http.ResponseWriter, r *http.Request) {
		actor, valid := simulationIdentity(r)
		if !valid {
			respond(w, nil, ErrForbidden, 403)
			return
		}
		var in Create
		if !rt.Decode(w, r, &in) {
			return
		}
		key := r.Header.Get("Idempotency-Key")
		if !transactionID.MatchString(key) {
			respond(w, nil, ErrInvalid, 422)
			return
		}
		v, replay, e := s.CreateAs(r.Context(), in, key, actor)
		status := 201
		if replay {
			status = 200
		}
		respond(w, v, e, status)
	})
	mux.HandleFunc("POST /internal/simulations/{id}/stop", func(w http.ResponseWriter, r *http.Request) {
		actor, valid := simulationIdentity(r)
		if !valid {
			respond(w, nil, ErrForbidden, 403)
			return
		}
		if !runID.MatchString(r.PathValue("id")) {
			respond(w, nil, pgx.ErrNoRows, 404)
			return
		}
		var in struct {
			Reason string `json:"reason"`
		}
		if !rt.Decode(w, r, &in) {
			return
		}
		v, e := s.StopAs(r.Context(), r.PathValue("id"), in.Reason, actor)
		respond(w, v, e, 200)
	})
	mux.HandleFunc("POST /internal/simulation/decision", func(w http.ResponseWriter, r *http.Request) {
		var in faults.Request
		if !rt.Decode(w, r, &in) {
			return
		}
		v, e := s.Decide(r.Context(), in)
		respond(w, v, e, 200)
	})
	return rt.RequireMutationToken(mux, mutationToken, func(r *http.Request) bool {
		return r.Method == http.MethodPost && (r.URL.Path == "/internal/simulations" ||
			(strings.HasPrefix(r.URL.Path, "/internal/simulations/") && strings.HasSuffix(r.URL.Path, "/stop")))
	})
}
