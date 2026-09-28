package incident

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestAlertCommandIdentity(t *testing.T) {
	a := AlertObservation{State: "firing", ActiveAt: time.Now().Add(-time.Minute), Labels: map[string]string{"alertname": "BusinessSuccessRateLow", "environment": "staging"}}
	input, key, err := alertCommand("primary", "development", "http://localhost:9090", a)
	if err != nil || input.Environment != "staging" || input.Service != "api-gateway" || input.AffectedUsers != nil {
		t.Fatalf("scope: %+v %v", input, err)
	}
	if !strings.Contains(input.Evidence[0].Summary, "paired 5m/1h or 30m/6h") || !strings.Contains(input.Evidence[0].Summary, "verify the durable SLO") {
		t.Fatalf("missing burn-rate scope: %+v", input.Evidence)
	}
	a.ActiveAt = a.ActiveAt.In(time.FixedZone("WIB", 7*60*60))
	_, same, err := alertCommand("primary", "development", "", a)
	if err != nil || key != same {
		t.Fatal("timezone changed episode identity")
	}
	for _, mutate := range []func(*AlertObservation){func(a *AlertObservation) { a.State = "pending" }, func(a *AlertObservation) { a.Labels = map[string]string{"alertname": "Unknown"} }, func(a *AlertObservation) { a.ActiveAt = time.Time{} }, func(a *AlertObservation) {
		a.Labels = map[string]string{"alertname": "BusinessSuccessRateLow", "environment": "invalid"}
	}} {
		copy := a
		mutate(&copy)
		if _, _, err := alertCommand("primary", "development", "", copy); err == nil {
			t.Fatal("invalid observation accepted")
		}
	}
}
func TestAlertPollResponseBounds(t *testing.T) {
	for _, body := range []string{`{"status":"error"}`, `not-json`, strings.Repeat("x", (1<<20)+1)} {
		t.Run(body[:8], func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if _, err := io.WriteString(w, body); err != nil {
					t.Log(err)
				}
			}))
			defer server.Close()
			err := pollAlerts(context.Background(), server.Client(), server.URL, Store{}, "primary", "development", "", slog.New(slog.NewTextHandler(io.Discard, nil)))
			if err == nil {
				t.Fatal("bad response accepted")
			}
		})
	}
}

func TestLatencyAlertScope(t *testing.T) {
	a := AlertObservation{State: "firing", ActiveAt: time.Now().Add(-time.Minute), Labels: map[string]string{"alertname": "ServiceLatencyHigh", "service": "payment-service"}}
	input, _, err := alertCommand("primary", "development", "", a)
	if err != nil || input.Service != "payment-service" || input.Environment != "development" || input.Severity != "SEV-2" || input.LatencyMS != nil || !strings.Contains(input.Evidence[0].Summary, "shared-runtime") {
		t.Fatalf("latency scope: %+v %v", input, err)
	}
	a.Labels["service"] = "unknown"
	if _, _, err := alertCommand("primary", "development", "", a); err == nil {
		t.Fatal("unknown service accepted")
	}
}

func TestAlertEvidenceScopesAndEscapesLabels(t *testing.T) {
	a := AlertObservation{State: "firing", ActiveAt: time.Now().Add(-time.Minute), Labels: map[string]string{"alertname": "ServiceLatencyHigh", "service": "payment-service", "instance": "host\"\\line\n"}}
	input, _, err := alertCommand("primary", "staging", "http://localhost:9090", a)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(input.Evidence[0].URL)
	if err != nil {
		t.Fatal(err)
	}
	expected := `ALERTS{alertstate="firing",alertname="ServiceLatencyHigh",instance="host\"\\line\n",service="payment-service"}`
	if u.Query().Get("query") != expected {
		t.Fatalf("query %q", u.Query().Get("query"))
	}
	if !strings.Contains(input.Evidence[0].Summary, "not a historical snapshot") {
		t.Fatal("missing temporal scope")
	}
	for _, name := range []string{"bad-name", "x} or vector(1)", "__name__", "alertstate"} {
		a.Labels[name] = "x"
		if _, _, err := alertCommand("primary", "staging", "http://localhost:9090", a); err == nil {
			t.Fatalf("accepted label %q", name)
		}
		delete(a.Labels, name)
	}
}

func TestLogExportLossScope(t *testing.T) {
	a := AlertObservation{State: "firing", ActiveAt: time.Now().Add(-time.Minute), Labels: map[string]string{"alertname": "LogExportLoss", "service": "simulation-service"}}
	input, _, err := alertCommand("primary", "development", "", a)
	if err != nil || input.Severity != "SEV-3" || input.Service != "simulation-service" || input.AffectedUsers != nil || !strings.Contains(input.Evidence[0].Summary, "shared-runtime") {
		t.Fatalf("scope %+v %v", input, err)
	}
}

func TestDeploymentServiceLogExportLossScope(t *testing.T) {
	a := AlertObservation{State: "firing", ActiveAt: time.Now().Add(-time.Minute), Labels: map[string]string{"alertname": "LogExportLoss", "service": "deployment-service"}}
	input, _, err := alertCommand("local-prometheus", "development", "http://localhost:9090", a)
	if err != nil || input.Service != "deployment-service" || input.Severity != "SEV-3" {
		t.Fatalf("deployment log-export alert lost: %+v, %v", input, err)
	}
}
