package incident

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"telcopulse/services/shared/runtime"
	"time"
)

// PrometheusWorker validates configuration and polls known firing alerts. An empty
// endpoint explicitly disables ingestion for native development.
func PrometheusWorker(endpoint, source, environment, viewer string) (runtime.Worker, error) {
	if endpoint == "" {
		return func(context.Context, *pgxpool.Pool, *slog.Logger) {}, nil
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("invalid PROMETHEUS_URL")
	}
	if source == "" || (environment != "development" && environment != "staging") {
		return nil, errors.New("alert source and environment are required")
	}
	// Validate the viewer before starting, rather than failing each observation.
	_, _, err = alertCommand(source, environment, viewer, AlertObservation{State: "firing", ActiveAt: time.Now(), Labels: map[string]string{"alertname": "NotificationConsumerLag"}})
	if err != nil {
		return nil, err
	}
	endpoint = strings.TrimRight(endpoint, "/") + "/api/v1/alerts"
	return func(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger) {
		client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("prometheus redirects are disabled") }}
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			pollCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
			err := pollAlerts(pollCtx, client, endpoint, Store{Pool: pool}, source, environment, viewer, log)
			cancel()
			if err != nil && ctx.Err() == nil {
				log.Warn("Prometheus incident ingestion failed", "error", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}, nil
}
func pollAlerts(ctx context.Context, client *http.Client, endpoint string, store Store, source, environment, viewer string, log *slog.Logger) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("fetch alert observations: %w", err)
	}
	defer func() {
		if err := response.Body.Close(); err != nil {
			log.Warn("close alert response", "error", err)
		}
	}()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("prometheus returned HTTP %d", response.StatusCode)
	}
	const maximum = 1 << 20
	data, err := io.ReadAll(io.LimitReader(response.Body, maximum+1))
	if err != nil {
		return err
	}
	if len(data) > maximum {
		return errors.New("prometheus alert response exceeds limit")
	}
	var payload struct {
		Status string `json:"status"`
		Data   struct {
			Alerts *[]AlertObservation `json:"alerts"`
		} `json:"data"`
	}
	if err = json.Unmarshal(data, &payload); err != nil {
		return errors.New("invalid Prometheus alert response")
	}
	if payload.Status != "success" || payload.Data.Alerts == nil || len(*payload.Data.Alerts) > 1000 {
		return errors.New("invalid Prometheus alert envelope")
	}
	var failures []error
	for _, a := range *payload.Data.Alerts {
		if a.State != "firing" {
			continue
		}
		switch a.Labels["alertname"] {
		case "LogExportLoss", "ServiceLatencyHigh", "BusinessSuccessRateLow", "ServiceUnavailable", "NotificationConsumerLag", "EventPublicationDelayed", "KafkaLagMeasurementUnavailable":
		default:
			continue
		}
		item, replay, err := store.IngestAlert(ctx, source, environment, viewer, a)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		if !replay {
			log.Info("incident created from Prometheus alert", "incident_id", item.ID, "alert", a.Labels["alertname"], "environment", item.Environment)
		}
	}
	return errors.Join(failures...)
}
