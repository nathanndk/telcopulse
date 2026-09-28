// Package httpapi exposes the domain through a bounded JSON REST API.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"telcopulse/services/api-gateway/internal/servicehealth"
	"telcopulse/services/api-gateway/internal/store"
	"telcopulse/services/api-gateway/internal/workflow"
	"telcopulse/services/shared/domain"
	"telcopulse/services/shared/rpc"
)

// Repository is the persistence boundary for HTTP handlers.
type Repository interface {
	Customers(context.Context) ([]domain.Customer, error)
	Packages(context.Context) ([]domain.Package, error)
	Purchase(context.Context, domain.Purchase, string) (domain.Transaction, bool, error)
	Transaction(context.Context, string) (domain.Transaction, error)
	Transactions(context.Context, string, string, string, int, int) (domain.TransactionPage, error)
	Overview(context.Context, string) (domain.Overview, error)
	AuditEvents(context.Context, store.AuditFilter) (store.AuditPage, error)
	WorkspaceSearch(context.Context, string, string) (store.WorkspaceResults, error)
}

// Server serves local development operators, optionally enforcing sessions at the gateway.
type Server struct {
	Auth           Authenticator
	RequireAuth    bool
	CookieSecure   bool
	ServiceHealth  *servicehealth.Client
	Simulations    *rpc.Client
	Incidents      *rpc.Client
	Deployments    *rpc.Client
	Notifications  *rpc.Client
	Metrics        http.Handler
	PurchaseBudget func(context.Context) (time.Duration, error)
	Repo           Repository
	Log            *slog.Logger
	Ready          func(context.Context) error
	Origin         string
}

// Handler configures routes, origin protection and request deadlines.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	s.authRoutes(mux)
	s.incidentRoutes(mux)
	s.deploymentRoutes(mux)
	s.simulationRoutes(mux)
	mux.HandleFunc("GET /api/v1/services/health", func(w http.ResponseWriter, r *http.Request) {
		result, err := s.ServiceHealth.Read(r.Context())
		if err != nil {
			s.write(w, 503, map[string]string{"error": "service telemetry unavailable"})
			return
		}
		s.write(w, 200, result)
	})
	if s.Metrics != nil {
		mux.Handle("GET /metrics", s.Metrics)
	}
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { s.write(w, 200, map[string]string{"status": "ok"}) })
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := s.Ready(r.Context()); err != nil {
			s.write(w, 503, map[string]string{"error": "database unavailable"})
			return
		}
		s.write(w, 200, map[string]string{"status": "ready"})
	})
	mux.HandleFunc("GET /api/v1/customers", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.Repo.Customers(r.Context())
		s.respond(w, v, e)
	})
	mux.HandleFunc("GET /api/v1/packages", func(w http.ResponseWriter, r *http.Request) { v, e := s.Repo.Packages(r.Context()); s.respond(w, v, e) })
	mux.HandleFunc("POST /api/v1/transactions", s.purchase)
	mux.HandleFunc("GET /api/v1/transactions/{id}", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.Repo.Transaction(r.Context(), r.PathValue("id"))
		s.respond(w, v, e)
	})
	mux.HandleFunc("GET /api/v1/transactions/{id}/notification", s.notificationStatus)
	mux.HandleFunc("GET /api/v1/notifications/dead-letters", s.deadLetters)
	mux.HandleFunc("GET /api/v1/notifications/dead-letters/{partition}/{offset}/replays", s.deadLetterReplayHistory)
	mux.HandleFunc("POST /api/v1/notifications/dead-letters/{partition}/{offset}/replay", s.replayDeadLetter)
	mux.HandleFunc("GET /api/v1/transactions", s.transactions)
	mux.HandleFunc("GET /api/v1/overview", func(w http.ResponseWriter, r *http.Request) {
		env, ok := s.environment(w, r)
		if !ok {
			return
		}
		v, e := s.Repo.Overview(r.Context(), env)
		s.respond(w, v, e)
	})
	mux.HandleFunc("GET /api/v1/audit", s.auditEvents)
	mux.HandleFunc("GET /api/v1/search", s.workspaceSearch)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		if origin := r.Header.Get("Origin"); origin != "" && origin != s.Origin {
			s.write(w, 403, map[string]string{"error": "origin not allowed"})
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") && r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			s.write(w, 403, map[string]string{"error": "cross-site request not allowed"})
			return
		}
		if s.RequireAuth && strings.HasPrefix(r.URL.Path, "/api/v1/") && !safeMethod(r.Method) && (s.Origin == "" || r.Header.Get("Origin") != s.Origin) {
			s.write(w, 403, map[string]string{"error": "same-origin request required"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		bounded := r.WithContext(ctx)
		if s.RequireAuth && strings.HasPrefix(r.URL.Path, "/api/v1/") && r.URL.Path != "/api/v1/auth/login" && r.URL.Path != "/api/v1/auth/status" {
			var ok bool
			bounded, ok = s.authenticate(w, bounded)
			if !ok {
				return
			}
		}
		mux.ServeHTTP(w, bounded)
		// Preserve the matched route for outer observability middleware.
		r.Pattern = bounded.Pattern
	})
}

var keyPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{16,100}$`)

func (s *Server) purchase(w http.ResponseWriter, r *http.Request) {
	if strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" {
		s.write(w, 415, map[string]string{"error": "use application/json"})
		return
	}
	key := r.Header.Get("Idempotency-Key")
	if !keyPattern.MatchString(key) {
		s.write(w, 400, map[string]string{"error": "Idempotency-Key must contain 16–100 letters, digits, underscores or hyphens"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var p domain.Purchase
	if err := dec.Decode(&p); err != nil {
		s.write(w, 400, map[string]string{"error": "invalid purchase payload"})
		return
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		s.write(w, 400, map[string]string{"error": "expected one JSON object"})
		return
	}
	if err := p.Validate(); err != nil {
		s.write(w, 422, map[string]string{"error": err.Error()})
		return
	}
	if s.PurchaseBudget != nil {
		retry, err := s.PurchaseBudget(r.Context())
		if err != nil {
			s.Log.Warn("purchase admission unavailable")
			s.write(w, 503, map[string]string{"error": "purchase admission unavailable; retry with the same idempotency key"})
			return
		}
		if retry > 0 {
			seconds := int((retry + time.Second - 1) / time.Second)
			w.Header().Set("Retry-After", strconv.Itoa(seconds))
			s.write(w, 429, map[string]string{"error": "synthetic purchase rate limit reached; retry with the same idempotency key"})
			return
		}
	}
	result, replayed, err := s.Repo.Purchase(r.Context(), p, key)
	if errors.Is(err, workflow.ErrPending) && result.ID != "" {
		result.Status = "PROCESSING"
		w.Header().Set("Location", "/api/v1/transactions/"+result.ID)
		s.write(w, http.StatusAccepted, result)
		return
	}
	if err != nil {
		s.respond(w, nil, err)
		return
	}
	code := http.StatusCreated
	if replayed {
		code = http.StatusOK
		w.Header().Set("Idempotency-Replayed", "true")
	}
	s.Log.Info("transaction completed", "service", "api-gateway", "environment", result.Environment, "transaction_id", result.ID, "replay_of", result.ReplayOf, "trace_id", result.TraceID, "msisdn_masked", result.MSISDN, "status", result.Status, "error_code", result.ErrorCode, "duration_ms", result.DurationMS, "version", "0.2.0", "idempotency_replayed", replayed)
	s.write(w, code, result)
}
func (s *Server) environment(w http.ResponseWriter, r *http.Request) (string, bool) {
	env := r.URL.Query().Get("environment")
	if env == "" {
		env = "development"
	}
	if env != "development" && env != "staging" {
		s.write(w, 400, map[string]string{"error": "invalid environment"})
		return "", false
	}
	return env, true
}
func (s *Server) transactions(w http.ResponseWriter, r *http.Request) {
	env, ok := s.environment(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	status := q.Get("status")
	if status != "" && status != "SUCCESS" && status != "FAILED" && status != "PROCESSING" {
		s.write(w, 400, map[string]string{"error": "invalid transaction status"})
		return
	}
	page, size := 1, 20
	var err error
	if q.Has("page") {
		page, err = strconv.Atoi(q.Get("page"))
		if err != nil || page < 1 || page > 100000 {
			s.write(w, 400, map[string]string{"error": "invalid page"})
			return
		}
	}
	if q.Has("page_size") {
		size, err = strconv.Atoi(q.Get("page_size"))
		if err != nil || size < 1 || size > 100 {
			s.write(w, 400, map[string]string{"error": "page_size must be between 1 and 100"})
			return
		}
	}
	search := q.Get("search")
	if len(search) > 100 {
		s.write(w, 400, map[string]string{"error": "search is too long"})
		return
	}
	v, e := s.Repo.Transactions(r.Context(), env, status, search, page, size)
	s.respond(w, v, e)
}
func (s *Server) respond(w http.ResponseWriter, v any, err error) {
	if err == nil {
		s.write(w, 200, v)
		return
	}
	if errors.Is(err, workflow.ErrPending) {
		s.write(w, 503, map[string]string{"error": err.Error()})
		return
	}
	if errors.Is(err, workflow.ErrInvalidReplay) {
		s.write(w, 422, map[string]string{"error": err.Error()})
		return
	}
	if errors.Is(err, store.ErrNotFound) {
		s.write(w, 404, map[string]string{"error": "resource not found"})
		return
	}
	if errors.Is(err, store.ErrConflict) {
		s.write(w, 409, map[string]string{"error": err.Error()})
		return
	}
	s.Log.Error("request failed", "error", err)
	s.write(w, 500, map[string]string{"error": "operation failed; try again or contact an administrator"})
}
func (s *Server) write(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		s.Log.Error("write response", "error", err)
	}
}
