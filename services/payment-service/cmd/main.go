package main

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	"log/slog"
	"net/http"
	"os"
	domainservice "telcopulse/services/payment-service"
	"telcopulse/services/shared/events"
	"telcopulse/services/shared/faults"
	"telcopulse/services/shared/rpc"
	"telcopulse/services/shared/runtime"
)

func main() {
	if err := run(); err != nil {
		slog.Error("service stopped", "error", err)
		os.Exit(1)
	}
}
func run() error {
	producer, err := events.NewProducer(os.Getenv("KAFKA_BROKERS"))
	if err != nil {
		return err
	}
	defer producer.Close()
	handler := domainservice.Handler
	if endpoint := os.Getenv("SIMULATION_URL"); endpoint != "" {
		if os.Getenv("APP_MODE") != "local" {
			return errors.New("simulation requires APP_MODE=local")
		}
		client := rpc.New(endpoint, os.Getenv("SERVICE_TOKEN"))
		handler = func(pool *pgxpool.Pool, log *slog.Logger) http.Handler {
			return domainservice.HandlerWithDecision(pool, log, func(ctx context.Context, in faults.Request) (faults.Decision, error) {
				var out faults.Decision
				err := client.Call(ctx, "POST", "/internal/simulation/decision", in, &out, "", in.TransactionID)
				if err == nil && out.Inject {
					log.Warn("synthetic payment fault selected", "scenario", out.Scenario, "delay_ms", out.DelayMS, "simulation_id", out.RunID, "deployment_id", out.DeploymentID, "release_version", out.Version, "transaction_id", in.TransactionID, "environment", in.Environment)
				}
				return out, err
			})
		}
	}
	return runtime.Run("payment-service", handler, func(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger) {
		events.PublishFrom(ctx, pool, log, events.PaymentOutbox, events.KafkaSender(producer))
	})
}
