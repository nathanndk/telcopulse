package incident

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"slices"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"telcopulse/services/shared/runtime"
)

// ingestedAlerts are the rule names that open incidents. Other routed alerts,
// such as infrastructure dependency signals, notify humans without guessing an
// affected application service.
var ingestedAlerts = []string{"LogExportLoss", "ServiceLatencyHigh", "BusinessSuccessRateLow", "PurchaseLatencyBudgetBurn", "NotificationFreshnessBudgetBurn", "ServiceUnavailable", "NotificationConsumerLag", "EventPublicationDelayed", "KafkaLagMeasurementUnavailable"}

func ingestible(name string) bool { return slices.Contains(ingestedAlerts, name) }

type alertIngester interface {
	IngestAlert(ctx context.Context, source, environment, viewer string, alert AlertObservation) (Incident, bool, error)
}

// alertmanagerNotification is the Alertmanager webhook v4 envelope. Annotations,
// generator URLs and fingerprints are ignored: incident identity uses labels and
// the firing start time only.
type alertmanagerNotification struct {
	Version string `json:"version"`
	Alerts  *[]struct {
		Status   string            `json:"status"`
		Labels   map[string]string `json:"labels"`
		StartsAt time.Time         `json:"startsAt"`
	} `json:"alerts"`
}

// AlertmanagerReceiver serves the Alertmanager webhook on a dedicated listener.
// Its bearer capability is separate from SERVICE_TOKEN, so the alert router can
// open detections but cannot read or edit incidents.
func AlertmanagerReceiver(addr, token, serviceToken, operatorToken, source, environment, viewer string) (runtime.Worker, error) {
	if len(token) < 32 || token == serviceToken || token == operatorToken {
		return nil, errors.New("distinct ALERT_WEBHOOK_TOKEN of at least 32 characters is required")
	}
	if _, _, err := net.SplitHostPort(addr); err != nil {
		return nil, errors.New("invalid ALERT_WEBHOOK_ADDR")
	}
	if source == "" || (environment != "development" && environment != "staging") {
		return nil, errors.New("alert source and environment are required")
	}
	if _, _, err := alertCommand(source, environment, viewer, AlertObservation{State: "firing", ActiveAt: time.Now(), Labels: map[string]string{"alertname": "NotificationConsumerLag"}}); err != nil {
		return nil, err
	}
	return func(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger) {
		server := &http.Server{Addr: addr, Handler: alertmanagerHandler(Store{Pool: pool}, token, source, environment, viewer, log), ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second}
		done := make(chan error, 1)
		go func() { done <- server.ListenAndServe() }()
		log.Info("Alertmanager receiver listening", "address", addr)
		select {
		case err := <-done:
			if !errors.Is(err, http.ErrServerClosed) {
				log.Error("Alertmanager receiver stopped", "error", err)
			}
		case <-ctx.Done():
			shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := server.Shutdown(shutdown); err != nil {
				log.Warn("Alertmanager receiver shutdown", "error", err)
			}
		}
	}, nil
}

func alertmanagerHandler(store alertIngester, token, source, environment, viewer string, log *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /alertmanager", func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+token)) != 1 {
			runtime.JSON(w, 401, map[string]string{"error": "alert webhook authentication required"})
			return
		}
		const maximum = 1 << 20
		data, err := io.ReadAll(io.LimitReader(r.Body, maximum+1))
		if err != nil || len(data) > maximum {
			runtime.JSON(w, 413, map[string]string{"error": "alert notification exceeds limit"})
			return
		}
		var payload alertmanagerNotification
		if json.Unmarshal(data, &payload) != nil || payload.Version != "4" || payload.Alerts == nil || len(*payload.Alerts) > 1000 {
			runtime.JSON(w, 400, map[string]string{"error": "invalid Alertmanager notification"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		created, existing, skipped := 0, 0, 0
		var failures []error
		for _, a := range *payload.Alerts {
			if a.Status != "firing" || !ingestible(a.Labels["alertname"]) {
				skipped++
				continue
			}
			item, replay, err := store.IngestAlert(ctx, source, environment, viewer, AlertObservation{Labels: a.Labels, State: "firing", ActiveAt: a.StartsAt})
			switch {
			case errors.Is(err, ErrInvalid):
				// A malformed alert would otherwise be retried forever by Alertmanager.
				log.Warn("Alertmanager alert rejected", "alert", a.Labels["alertname"], "error", err)
				skipped++
			case err != nil:
				failures = append(failures, err)
			case replay:
				existing++
			default:
				created++
				log.Info("incident created from Alertmanager notification", "incident_id", item.ID, "alert", a.Labels["alertname"], "environment", item.Environment)
			}
		}
		if len(failures) > 0 {
			// A non-2xx response makes Alertmanager retry the whole group; ingestion is idempotent.
			log.Warn("Alertmanager ingestion failed", "error", errors.Join(failures...))
			runtime.JSON(w, 503, map[string]string{"error": "incident storage unavailable"})
			return
		}
		runtime.JSON(w, 200, map[string]int{"created": created, "existing": existing, "skipped": skipped})
	})
	return mux
}
