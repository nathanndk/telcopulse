package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"telcopulse/services/shared/faults"
	"telcopulse/services/shared/rpc"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/twmb/franz-go/pkg/kgo"
	domainservice "telcopulse/services/notification-service"
	"telcopulse/services/shared/events"
	"telcopulse/services/shared/runtime"
)

func main() {
	if err := run(); err != nil {
		slog.Error("service stopped", "error", err)
		os.Exit(1)
	}
}
func run() error {
	replayToken := os.Getenv("NOTIFICATION_OPERATOR_TOKEN")
	if len(replayToken) < 32 || replayToken == os.Getenv("SERVICE_TOKEN") {
		return errors.New("distinct NOTIFICATION_OPERATOR_TOKEN of at least 32 characters is required")
	}
	var decide domainservice.DecisionFunc
	if endpoint := os.Getenv("SIMULATION_URL"); endpoint != "" {
		if os.Getenv("APP_MODE") != "local" {
			return errors.New("simulation requires APP_MODE=local")
		}
		simulation := rpc.New(endpoint, os.Getenv("SERVICE_TOKEN"))
		decide = func(ctx context.Context, in faults.Request) (faults.Decision, error) {
			var out faults.Decision
			err := simulation.Call(ctx, "POST", "/internal/simulation/decision", in, &out, "", in.TransactionID)
			return out, err
		}
	}
	brokers := os.Getenv("KAFKA_BROKERS")
	if brokers == "" {
		brokers = "127.0.0.1:19092"
	}
	client, err := kgo.NewClient(kgo.SeedBrokers(strings.Split(brokers, ",")...), kgo.ConsumerGroup("telcopulse-notifications-v1"), kgo.ConsumeTopics(events.PurchaseTopic), kgo.DisableAutoCommit(), kgo.BlockRebalanceOnPoll(), kgo.FetchMaxBytes(1<<20))
	if err != nil {
		return err
	}
	defer client.Close()
	producer, err := events.NewProducer(brokers)
	if err != nil {
		return err
	}
	defer producer.Close()
	return runtime.Run("notification-service", func(pool *pgxpool.Pool, log *slog.Logger) http.Handler {
		return domainservice.HandlerWithReplay(pool, log, replayToken)
	}, func(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger) {
		domainservice.ConsumeWithDecision(ctx, pool, log, client, decide)
	}, func(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger) {
		events.PublishFrom(ctx, pool, log, events.NotificationOutbox, events.KafkaSender(producer))
	})
}
