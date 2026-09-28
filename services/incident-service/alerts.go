package incident

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
)

var alertLabelName = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// AlertObservation is a Prometheus alert episode; changing values/annotations
// are intentionally excluded from incident creation identity and metadata.
type AlertObservation struct {
	Labels   map[string]string `json:"labels"`
	State    string            `json:"state"`
	ActiveAt time.Time         `json:"activeAt"`
}

// IngestAlert persists the first detection of an episode. Later observations
// return the current incident without overwriting operator-owned fields.
func (s Store) IngestAlert(ctx context.Context, source, environment, viewer string, alert AlertObservation) (Incident, bool, error) {
	input, key, err := alertCommand(source, environment, viewer, alert)
	if err != nil {
		return Incident{}, false, err
	}
	lookup := func() (Incident, error) {
		var item Incident
		var data []byte
		err := s.Pool.QueryRow(ctx, `SELECT document FROM incident.records WHERE creation_key=$1`, key).Scan(&data)
		if err != nil {
			return item, err
		}
		err = json.Unmarshal(data, &item)
		return item, err
	}
	existing, err := lookup()
	if err == nil {
		return existing, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Incident{}, false, err
	}
	result, replay, err := s.Create(ctx, input, key, "prometheus-alert")
	if errors.Is(err, ErrConflict) {
		existing, readErr := lookup()
		return existing, true, readErr
	}
	return result, replay, err
}

func alertCommand(source, environment, viewer string, a AlertObservation) (Create, string, error) {
	var input Create
	if a.State != "firing" || !bounded(source, 1, 100) || a.ActiveAt.IsZero() || a.ActiveAt.After(time.Now().Add(time.Minute)) || len(a.Labels) > 25 {
		return input, "", invalid(errors.New("invalid firing alert"))
	}
	for k, v := range a.Labels {
		if !bounded(k, 1, 100) || !alertLabelName.MatchString(k) || k == "__name__" || k == "alertstate" || len(v) > 200 {
			return input, "", invalid(errors.New("invalid alert labels"))
		}
	}
	service := a.Labels["service"]
	severity := "SEV-2"
	switch a.Labels["alertname"] {
	case "BusinessSuccessRateLow":
		service = "api-gateway"
		environment = a.Labels["environment"]
	case "ServiceUnavailable":
		severity = "SEV-1"
	case "NotificationConsumerLag":
		service = "notification-service"
	case "EventPublicationDelayed", "ServiceLatencyHigh":
	case "LogExportLoss":
		severity = "SEV-3"
	case "KafkaLagMeasurementUnavailable":
		service = "notification-service"
		severity = "SEV-3"
	default:
		return input, "", invalid(errors.New("unsupported alert rule"))
	}
	if !slices.Contains([]string{"development", "staging"}, environment) || !slices.Contains([]string{"api-gateway", "subscriber-service", "package-service", "payment-service", "notification-service", "incident-service", "simulation-service", "deployment-service"}, service) {
		return input, "", invalid(errors.New("invalid alert scope"))
	}
	identity, err := json.Marshal(struct {
		Source   string
		Labels   map[string]string
		ActiveAt time.Time
	}{source, a.Labels, a.ActiveAt.UTC()})
	if err != nil {
		return input, "", err
	}
	digest := sha256.Sum256(identity)
	key := "alert-" + hex.EncodeToString(digest[:])
	title := fmt.Sprintf("%s · %s", a.Labels["alertname"], service)
	evidence := Evidence{Kind: "metric", Summary: fmt.Sprintf("Prometheus %s firing since %s; episode %s", a.Labels["alertname"], a.ActiveAt.UTC().Format(time.RFC3339), key)}
	if a.Labels["alertname"] == "BusinessSuccessRateLow" {
		evidence.Summary += "; synthetic purchase failure budget burn exceeded the paired 5m/1h or 30m/6h threshold for the 99.9% objective. Prometheus observes process-local terminal outcomes; verify the durable SLO and customer impact before escalation."
	}
	if a.Labels["alertname"] == "ServiceLatencyHigh" {
		evidence.Summary += "; shared-runtime HTTP P95 above one second for thirty seconds, with at least twenty requests in five minutes. Incident environment is the configured routing scope, not measured customer impact."
	}
	if a.Labels["alertname"] == "LogExportLoss" {
		evidence.Summary += "; shared-runtime log export failed or dropped events. Check stdout for missing evidence; incident environment is routing scope, not measured customer impact."
	}
	if viewer != "" {
		u, err := url.Parse(viewer)
		if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") || u.RawQuery != "" || u.Fragment != "" {
			return input, "", invalid(errors.New("invalid Prometheus viewer URL"))
		}
		u.Path = "/api/v1/query"
		q := url.Values{}
		names := make([]string, 0, len(a.Labels))
		for name := range a.Labels {
			names = append(names, name)
		}
		slices.Sort(names)
		selectors := []string{`alertstate="firing"`}
		for _, name := range names {
			selectors = append(selectors, fmt.Sprintf("%s=%q", name, a.Labels[name]))
		}
		q.Set("query", "ALERTS{"+strings.Join(selectors, ",")+"}")
		evidence.Summary += "; link shows current firing state for these alert labels, not a historical snapshot"
		u.RawQuery = q.Encode()
		evidence.URL = u.String()
	}
	detected := a.ActiveAt.UTC()
	input = Create{Fields: Fields{Title: title, Severity: severity, Impact: "Automated detection. Customer impact is not yet assessed.", Evidence: []Evidence{evidence}, ActionItems: []ActionItem{}}, Environment: environment, Service: service, DetectedAt: &detected}
	return input, key, nil
}
