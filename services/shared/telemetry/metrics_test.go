package telemetry

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRouteLabelsExcludeIdentifiers(t *testing.T) {
	m := New("test-service", nil)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /transactions/{id}", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) })
	h := m.Wrap(mux)
	for _, id := range []string{"private-customer-123", "private-customer-456"} {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/transactions/"+id+"?msisdn=628123456789", nil))
	}
	w := httptest.NewRecorder()
	m.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/metrics", nil))
	body := w.Body.String()
	if strings.Contains(body, "private-customer") || strings.Contains(body, "628123456789") {
		t.Fatal("PII leaked into metric labels")
	}
	if !strings.Contains(body, `http_requests_total{method="GET",route="GET /transactions/{id}",service="test-service",status="503"} 2`) {
		t.Fatalf("missing normalized error count: %s", body)
	}
}
func TestProbesExcluded(t *testing.T) {
	m := New("test-service", nil)
	h := m.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	for _, path := range []string{"/healthz", "/readyz", "/metrics"} {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", path, nil))
	}
	families, err := m.Registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range families {
		if f.GetName() == "http_requests_total" {
			t.Fatal("probe counted as traffic")
		}
	}
}

func TestNotificationFreshnessScope(t *testing.T) {
	if strings.Contains(scrape(New("api-gateway", nil)), "notification_delivery_freshness_seconds") {
		t.Fatal("freshness exported outside notification-service")
	}
	m := New("notification-service", nil)
	before := scrape(m)
	if !strings.Contains(before, `notification_delivery_freshness_seconds_count{environment="staging",service="notification-service"} 0`) {
		t.Fatalf("idle environment not initialized: %s", before)
	}
	NotificationDelivered("development", 3)
	NotificationDelivered("development", -1)
	NotificationDelivered("production", 3)
	body := scrape(m)
	if !strings.Contains(body, `notification_delivery_freshness_seconds_bucket{environment="development",service="notification-service",le="0.5"}`) || strings.Contains(body, `environment="production"`) {
		t.Fatalf("unexpected freshness series: %s", body)
	}
}

func scrape(m *Metrics) string {
	w := httptest.NewRecorder()
	m.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/metrics", nil))
	return w.Body.String()
}
