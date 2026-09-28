package main

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	auth "telcopulse/services/auth-service"
	"telcopulse/services/shared/runtime"
)

func main() {
	if os.Getenv("APP_MODE") != "local" {
		slog.Error("auth-service requires APP_MODE=local until SSO is configured")
		os.Exit(1)
	}
	if len(os.Args) > 1 {
		if len(os.Args) != 2 || os.Args[1] != "bootstrap" {
			slog.Error("invalid auth-service command")
			os.Exit(1)
		}
		if err := bootstrap(); err != nil {
			slog.Error("local administrator bootstrap failed", "error", err)
			os.Exit(1)
		}
		return
	}
	dummy, err := auth.DummyPasswordHash()
	if err != nil {
		slog.Error("password verifier initialization failed", "error", err)
		os.Exit(1)
	}
	if err = runtime.Run("auth-service", func(pool *pgxpool.Pool, log *slog.Logger) http.Handler {
		return auth.Handler(pool, log, dummy)
	}); err != nil {
		slog.Error("auth-service stopped", "error", err)
		os.Exit(1)
	}
}

func bootstrap() error {
	data, err := io.ReadAll(io.LimitReader(os.Stdin, 257))
	if err != nil || len(data) > 256 {
		return auth.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		return err
	}
	defer pool.Close()
	created, err := auth.BootstrapLocal(ctx, pool, "local-admin", string(data))
	if err != nil {
		return err
	}
	if created {
		slog.Info("local administrator created")
	} else {
		slog.Info("local administrator already exists; bootstrap did not change credentials")
	}
	return nil
}
