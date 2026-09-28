// Package runtime provides common lifecycle, database and JSON plumbing for domain services.
package runtime

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"telcopulse/services/shared/logexport"
	"telcopulse/services/shared/telemetry"
)

// JSON writes a bounded domain response.
func JSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		slog.Error("encode response", "error", err)
	}
}

// Decode rejects unknown fields, trailing input and oversized requests.
func Decode(w http.ResponseWriter, r *http.Request, v any) bool {
	return DecodeLimit(w, r, v, 8192)
}

// DecodeLimit applies an explicit service-specific body bound before strict decoding.
func DecodeLimit(w http.ResponseWriter, r *http.Request, v any, limit int64) bool {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		JSON(w, 400, map[string]string{"error": "invalid request"})
		return false
	}
	if err := d.Decode(&struct{}{}); err != io.EOF {
		JSON(w, 400, map[string]string{"error": "expected one object"})
		return false
	}
	return true
}

// Protect authenticates internal service calls and bounds execution time.
func Protect(next http.Handler, token string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" && r.URL.Path != "/readyz" && r.URL.Path != "/metrics" {
			got := r.Header.Get("Authorization")
			if token == "" || subtle.ConstantTimeCompare([]byte(got), []byte("Bearer "+token)) != 1 {
				JSON(w, 401, map[string]string{"error": "service authentication required"})
				return
			}
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		bounded := r.WithContext(ctx)
		next.ServeHTTP(w, bounded)
		// Preserve the matched route for outer observability middleware.
		r.Pattern = bounded.Pattern
	})
}

// MutationTokenHeader carries a service-specific capability for operator
// mutations. It is separate from the shared internal read/decision token.
const MutationTokenHeader = "X-Telcopulse-Mutation-Token"

// RequireMutationToken limits selected operator routes to a caller holding
// the service-specific mutation capability in addition to the shared token.
func RequireMutationToken(next http.Handler, token string, selected func(*http.Request) bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if selected(r) && (len(token) < 32 || subtle.ConstantTimeCompare([]byte(r.Header.Get(MutationTokenHeader)), []byte(token)) != 1) {
			JSON(w, 401, map[string]string{"error": "operator mutation authentication required"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Worker runs background service work until cancellation.
type Worker func(context.Context, *pgxpool.Pool, *slog.Logger)

// Run starts HTTP and background workers with bounded pooling and graceful shutdown.
func Run(name string, handler func(*pgxpool.Pool, *slog.Logger) http.Handler, workers ...Worker) error {
	logger, exporter, err := logexport.LoggerFromEnv(name)
	if err != nil {
		return err
	}
	defer logexport.Shutdown(exporter)
	slog.SetDefault(logger)
	token := os.Getenv("SERVICE_TOKEN")
	if len(token) < 32 {
		return errors.New("SERVICE_TOKEN of at least 32 characters is required")
	}
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		return errors.New("DATABASE_URL is required")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	shutdownTracing, err := telemetry.InstallTracing(ctx, name, logger)
	if err != nil {
		return err
	}
	defer shutdownTracing()
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return err
	}
	cfg.ConnConfig.Tracer = telemetry.DatabaseTracer{}
	cfg.MaxConns = 8
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return err
	}
	defer pool.Close()
	initCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	err = pool.Ping(initCtx)
	cancel()
	if err != nil {
		return err
	}
	workerCtx, workerCancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	for _, worker := range workers {
		wg.Add(1)
		go func() { defer wg.Done(); worker(workerCtx, pool, logger) }()
	}
	defer func() { workerCancel(); wg.Wait() }()
	metrics := telemetry.New(name, pool)
	logexport.Register(metrics.Registry, exporter)
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", metrics.Handler())
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { JSON(w, 200, map[string]string{"status": "ok"}) })
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := pool.Ping(r.Context()); err != nil {
			JSON(w, 503, map[string]string{"error": "database unavailable"})
			return
		}
		JSON(w, 200, map[string]string{"status": "ready"})
	})
	mux.Handle("/", handler(pool, logger))
	addr := os.Getenv("LISTEN_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8080"
	}
	server := &http.Server{Addr: addr, Handler: metrics.Wrap(telemetry.TraceHTTP(Protect(mux, token), logger)), ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second}
	done := make(chan error, 1)
	go func() { logger.Info("listening", "address", addr); done <- server.ListenAndServe() }()
	select {
	case err = <-done:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return server.Shutdown(c)
	}
	return nil
}
