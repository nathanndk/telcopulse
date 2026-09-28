package main

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"log/slog"
	"net/http"
	"os"
	"telcopulse/services/shared/runtime"
	simulation "telcopulse/services/simulation-service"
)

func main() {
	if os.Getenv("APP_MODE") != "local" {
		slog.Error("simulation service requires APP_MODE=local")
		os.Exit(1)
	}
	mutationToken := os.Getenv("SIMULATION_OPERATOR_TOKEN")
	if len(mutationToken) < 32 || mutationToken == os.Getenv("SERVICE_TOKEN") {
		slog.Error("SIMULATION_OPERATOR_TOKEN must be distinct and at least 32 characters")
		os.Exit(1)
	}
	worker, err := simulation.DeploymentWorker(os.Getenv("DEPLOYMENT_URL"), os.Getenv("SERVICE_TOKEN"))
	if err != nil {
		slog.Error("deployment reporting configuration invalid", "error", err)
		os.Exit(1)
	}
	handler := func(pool *pgxpool.Pool, log *slog.Logger) http.Handler {
		return simulation.Handler(pool, log, mutationToken)
	}
	if err := runtime.Run("simulation-service", handler, worker); err != nil {
		slog.Error("simulation stopped", "error", err)
		os.Exit(1)
	}
}
