package main

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"log/slog"
	"net/http"
	"os"
	incident "telcopulse/services/incident-service"
	"telcopulse/services/shared/runtime"
)

func main() {
	if os.Getenv("APP_MODE") != "local" {
		slog.Error("APP_MODE=local required until operator authentication is implemented")
		os.Exit(1)
	}
	mutationToken := os.Getenv("INCIDENT_OPERATOR_TOKEN")
	if len(mutationToken) < 32 || mutationToken == os.Getenv("SERVICE_TOKEN") {
		slog.Error("INCIDENT_OPERATOR_TOKEN must be distinct and at least 32 characters")
		os.Exit(1)
	}
	worker, err := incident.PrometheusWorker(os.Getenv("PROMETHEUS_URL"), os.Getenv("PROMETHEUS_SOURCE"), os.Getenv("PROMETHEUS_ENVIRONMENT"), os.Getenv("PROMETHEUS_VIEWER_URL"))
	if err != nil {
		slog.Error("invalid alert ingestion configuration", "error", err)
		os.Exit(1)
	}
	searcher, err := incident.SplunkSearchFromEnv()
	if err != nil {
		slog.Error("invalid Splunk Search configuration", "error", err)
		os.Exit(1)
	}
	traces, err := incident.JaegerSearchFromEnv()
	if err != nil {
		slog.Error("invalid Jaeger query configuration", "error", err)
		os.Exit(1)
	}
	metrics, err := incident.PrometheusRangeFromEnv()
	if err != nil {
		slog.Error("invalid Prometheus metric query configuration", "error", err)
		os.Exit(1)
	}
	infrastructure, err := incident.DatadogMetricsFromEnv()
	if err != nil {
		slog.Error("invalid Datadog metrics query configuration", "error", err)
		os.Exit(1)
	}
	handler := func(pool *pgxpool.Pool, log *slog.Logger) http.Handler {
		return incident.HandlerWithAllSources(pool, log, mutationToken, searcher, traces, metrics, infrastructure)
	}
	if err := runtime.Run("incident-service", handler, worker); err != nil {
		slog.Error("incident service stopped", "error", err)
		os.Exit(1)
	}
}
