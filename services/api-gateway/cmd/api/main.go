// Command api runs the TelcoPulse local development gateway.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"telcopulse/services/api-gateway/internal/httpapi"
	"telcopulse/services/api-gateway/internal/servicehealth"
	"telcopulse/services/api-gateway/internal/store"
	"telcopulse/services/api-gateway/internal/workflow"
	"telcopulse/services/shared/ephemeral"
	"telcopulse/services/shared/events"
	"telcopulse/services/shared/logexport"
	"telcopulse/services/shared/rpc"
	"telcopulse/services/shared/telemetry"
	"time"
)

func main() {
	log, exporter, err := logexport.LoggerFromEnv("api-gateway")
	if err != nil {
		slog.Error("logger configuration invalid")
		os.Exit(1)
	}
	if err := run(log, exporter); err != nil {
		log.Error("gateway stopped", "error", err)
		logexport.Shutdown(exporter)
		os.Exit(1)
	}
	logexport.Shutdown(exporter)
}
func run(log *slog.Logger, exporter *logexport.Queue) error {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		return errors.New("DATABASE_URL is required")
	}
	if os.Getenv("APP_MODE") != "local" {
		return errors.New("APP_MODE=local is required; authentication must be implemented before non-local deployment")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	shutdownTracing, err := telemetry.InstallTracing(ctx, "api-gateway", log)
	if err != nil {
		return err
	}
	defer shutdownTracing()
	initCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	db, err := store.Open(initCtx, url)
	if err != nil {
		return err
	}
	defer db.Pool.Close()
	if os.Getenv("MIGRATE_ONLY") == "true" {
		log.Info("database migrations applied")
		return nil
	}
	token := os.Getenv("SERVICE_TOKEN")
	if len(token) < 32 {
		return errors.New("SERVICE_TOKEN of at least 32 characters is required")
	}
	incidentMutationToken := os.Getenv("INCIDENT_OPERATOR_TOKEN")
	simulationMutationToken := os.Getenv("SIMULATION_OPERATOR_TOKEN")
	notificationMutationToken := os.Getenv("NOTIFICATION_OPERATOR_TOKEN")
	if len(incidentMutationToken) < 32 || len(simulationMutationToken) < 32 ||
		len(notificationMutationToken) < 32 || incidentMutationToken == token || simulationMutationToken == token || notificationMutationToken == token ||
		incidentMutationToken == simulationMutationToken || incidentMutationToken == notificationMutationToken || simulationMutationToken == notificationMutationToken {
		return errors.New("distinct incident, simulation and notification operator tokens of at least 32 characters are required")
	}
	for _, key := range []string{"SUBSCRIBER_URL", "PACKAGE_URL", "PAYMENT_URL", "INCIDENT_URL", "DEPLOYMENT_URL", "NOTIFICATION_URL"} {
		if os.Getenv(key) == "" {
			return errors.New(key + " is required")
		}
	}
	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		redisURL = "redis://127.0.0.1:16379/0"
	}
	cache, err := ephemeral.Open(redisURL, log)
	if err != nil {
		return err
	}
	defer func() {
		if e := cache.Client.Close(); e != nil {
			log.Error("close Redis", "error", e)
		}
	}()
	limit := 60
	if value := os.Getenv("PURCHASES_PER_MINUTE"); value != "" {
		limit, err = strconv.Atoi(value)
		if err != nil || limit < 1 || limit > 100000 {
			return errors.New("PURCHASES_PER_MINUTE must be between 1 and 100000")
		}
	}
	metrics := telemetry.New("api-gateway", db.Pool)
	logexport.Register(metrics.Registry, exporter)
	paymentClient := rpc.New(os.Getenv("PAYMENT_URL"), token)
	paymentClient.HTTP.Timeout = 6 * time.Second
	flow := &workflow.Service{Metrics: metrics, Store: db, Cache: cache, Subscriber: rpc.New(os.Getenv("SUBSCRIBER_URL"), token), Package: rpc.New(os.Getenv("PACKAGE_URL"), token), Payment: paymentClient, Log: log}
	brokers := os.Getenv("KAFKA_BROKERS")
	if brokers == "" {
		brokers = "127.0.0.1:19092"
	}
	producer, err := events.NewProducer(brokers)
	if err != nil {
		return err
	}
	metrics.Kafka(producer)
	recoveryCtx, recoveryCancel := context.WithCancel(ctx)
	var recovery sync.WaitGroup
	recovery.Add(2)
	go func() { defer recovery.Done(); events.Publish(recoveryCtx, db.Pool, log, events.KafkaSender(producer)) }()
	go func() { defer recovery.Done(); flow.Recover(recoveryCtx) }()
	defer func() { recoveryCancel(); producer.Close(); recovery.Wait() }()

	addr := os.Getenv("LISTEN_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8080"
	}
	origin := os.Getenv("WEB_ORIGIN")
	if origin == "" {
		origin = "http://localhost:3000"
	}
	health, err := servicehealth.New(os.Getenv("PROMETHEUS_URL"))
	if err != nil {
		return err
	}
	var simulations *rpc.Client
	if endpoint := os.Getenv("SIMULATION_URL"); endpoint != "" {
		simulations = rpc.New(endpoint, token)
		simulations.MutationToken = simulationMutationToken
	}
	incidents := rpc.New(os.Getenv("INCIDENT_URL"), token)
	incidents.MutationToken = incidentMutationToken
	notifications := rpc.New(os.Getenv("NOTIFICATION_URL"), token)
	notifications.MutationToken = notificationMutationToken
	api := &httpapi.Server{Simulations: simulations, ServiceHealth: health, Incidents: incidents, Deployments: rpc.New(os.Getenv("DEPLOYMENT_URL"), token), Notifications: notifications, Metrics: metrics.Handler(), Repo: flow, Log: log, Ready: db.Pool.Ping, Origin: origin, PurchaseBudget: func(ctx context.Context) (time.Duration, error) {
		return cache.Allow(ctx, "synthetic-purchase", limit, time.Minute)
	}}
	authRequired := os.Getenv("AUTH_REQUIRED")
	if authRequired != "" && authRequired != "false" && authRequired != "true" {
		return errors.New("AUTH_REQUIRED must be true or false")
	}
	if authRequired == "true" {
		endpoint := os.Getenv("AUTH_URL")
		if endpoint == "" || origin == "" {
			return errors.New("AUTH_URL and WEB_ORIGIN are required when AUTH_REQUIRED=true")
		}
		api.Auth = httpapi.RemoteAuth{Client: rpc.New(endpoint, token)}
		api.RequireAuth = true
		api.CookieSecure = strings.HasPrefix(origin, "https://")
	}
	server := &http.Server{Addr: addr, Handler: metrics.Wrap(telemetry.TraceHTTP(api.Handler(), log)), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16384}
	errCh := make(chan error, 1)
	go func() {
		log.Info("gateway listening", "address", addr, "mode", "local")
		errCh <- server.ListenAndServe()
	}()
	select {
	case err = <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err = server.Shutdown(shutdownCtx); err != nil {
			return err
		}
	}
	return nil
}
