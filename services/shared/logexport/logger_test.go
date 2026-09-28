package logexport

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

func TestConfiguredLoggerAndMetrics(t *testing.T) {
	received := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Event map[string]any `json:"event"`
		}
		if r.Header.Get("Authorization") != "Splunk integration-only-token" {
			t.Error("missing file token")
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			t.Error("invalid event")
		}
		received <- body.Event
		_, _ = fmt.Fprint(w, `{"code":0}`)
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte("integration-only-token\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("APP_MODE", "local")
	t.Setenv("SPLUNK_HEC_URL", server.URL+"/services/collector/event")
	t.Setenv("SPLUNK_HEC_TOKEN", "")
	t.Setenv("SPLUNK_HEC_TOKEN_FILE", path)
	logger, q, err := LoggerFromEnv("payment-service")
	if err != nil {
		t.Fatal(err)
	}
	registry := prometheus.NewRegistry()
	Register(registry, q)
	logger.Info("transaction completed", "transaction_id", "TXN-test", "error_code", "DB_TIMEOUT", "msisdn_masked", "62812*****123")
	Shutdown(q)
	var event map[string]any
	select {
	case event = <-received:
	case <-time.After(time.Second):
		t.Fatal("receiver did not receive log")
	}
	if event["service"] != "payment-service" || event["transaction_id"] != "TXN-test" || event["error_code"] != "DB_TIMEOUT" || event["msisdn_masked"] != "62812*****123" {
		t.Fatal(event)
	}
	families, err := registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	accepted := false
	pending := false
	for _, family := range families {
		for _, metric := range family.Metric {
			if family.GetName() == "log_export_pending" {
				pending = metric.GetGauge().GetValue() == 0
			}
			if family.GetName() == "log_export_events_total" {
				for _, label := range metric.Label {
					if label.GetValue() == "accepted" {
						accepted = metric.GetCounter().GetValue() == 1
					}
				}
			}
		}
	}
	if !accepted || !pending {
		t.Fatal("delivery metrics missing")
	}
}

func TestLoggerConfigurationFailure(t *testing.T) {
	t.Setenv("SPLUNK_HEC_URL", "")
	t.Setenv("SPLUNK_HEC_TOKEN", "")
	t.Setenv("SPLUNK_HEC_TOKEN_FILE", "")
	_, q, err := LoggerFromEnv("test")
	if err != nil || q != nil {
		t.Fatal("export must default off")
	}
	t.Setenv("SPLUNK_HEC_TOKEN", "secret")
	if _, _, err = LoggerFromEnv("test"); err == nil {
		t.Fatal("partial configuration accepted")
	}
	t.Setenv("SPLUNK_HEC_TOKEN_FILE", "missing")
	if _, _, err = LoggerFromEnv("test"); err == nil {
		t.Fatal("ambiguous token sources accepted")
	}
}
