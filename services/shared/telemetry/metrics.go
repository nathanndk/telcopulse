// Package telemetry records operational measurements without subscriber identifiers.
package telemetry

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics owns a service-local registry; counters reset when its process restarts.
type Metrics struct {
	Registry         *prometheus.Registry
	requests         *prometheus.CounterVec
	duration         *prometheus.HistogramVec
	business         *prometheus.CounterVec
	businessDuration *prometheus.HistogramVec
}

// notificationFreshness is process-global because Kafka delivery runs outside the
// HTTP handler that owns *Metrics; New registers it for notification-service only.
var notificationFreshness = prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "notification_delivery_freshness_seconds", Help: "Time from the committed purchase outcome to its first durable notification receipt; excludes redelivery and operator reprocessing.", Buckets: []float64{.5, 1, 2, 5, 10, 30, 60, 300, 900}}, []string{"environment"})

// NotificationDelivered records the first durable receipt for a purchase outcome.
// Clock skew between producer and consumer can make small ages negative; they count as zero.
func NotificationDelivered(environment string, seconds float64) {
	if environment != "development" && environment != "staging" {
		return
	}
	notificationFreshness.WithLabelValues(environment).Observe(max(seconds, 0))
}

// New registers bounded HTTP, business, process and pool metrics.
func New(service string, pool *pgxpool.Pool) *Metrics {
	registry := prometheus.NewRegistry()
	registerer := prometheus.WrapRegistererWith(prometheus.Labels{"service": service}, registry)
	m := &Metrics{Registry: registry,
		requests:         prometheus.NewCounterVec(prometheus.CounterOpts{Name: "http_requests_total", Help: "Completed HTTP requests excluding probes and scrapes."}, []string{"method", "route", "status"}),
		duration:         prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "http_request_duration_seconds", Help: "HTTP handler duration in seconds.", Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2, 5, 10}}, []string{"method", "route"}),
		business:         prometheus.NewCounterVec(prometheus.CounterOpts{Name: "transaction_total", Help: "Newly committed business outcomes observed by this process; excludes replays."}, []string{"environment", "status", "failure_class"}),
		businessDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "transaction_duration_seconds", Help: "Time from purchase creation through completion, including recovery.", Buckets: []float64{.05, .1, .25, .5, 1, 2, 5, 10, 30, 60, 300}}, []string{"environment", "status"}),
	}
	registerer.MustRegister(m.requests, m.duration, collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	if service == "api-gateway" {
		registerer.MustRegister(m.business, m.businessDuration)
		for _, env := range []string{"development", "staging"} {
			m.business.WithLabelValues(env, "SUCCESS", "none").Add(0)
			m.business.WithLabelValues(env, "FAILED", "technical").Add(0)
			m.business.WithLabelValues(env, "FAILED", "business").Add(0)
		}
	}
	if service == "notification-service" {
		registerer.MustRegister(notificationFreshness)
		for _, env := range []string{"development", "staging"} {
			notificationFreshness.WithLabelValues(env)
		}
	}
	if pool != nil {
		if collector := newOutboxCollector(pool, service); collector != nil {
			registerer.MustRegister(collector)
		}
		registerer.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "database_connection_usage", Help: "Connections currently acquired from this service pool."}, func() float64 { return float64(pool.Stat().AcquiredConns()) }), prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "database_connections_max", Help: "Configured maximum pool connections."}, func() float64 { return float64(pool.Stat().MaxConns()) }))
	}
	return m
}

// Handler exports measurements using the standard Prometheus exposition format.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.Registry, promhttp.HandlerOpts{})
}

// Business records only known terminal outcomes, never arbitrary input labels.
// failureClass is "none" for SUCCESS and "technical" or "business" for FAILED.
func (m *Metrics) Business(environment, status, failureClass string, seconds float64) {
	if (environment != "development" && environment != "staging") ||
		(status == "SUCCESS" && failureClass != "none") ||
		(status == "FAILED" && failureClass != "technical" && failureClass != "business") ||
		(status != "SUCCESS" && status != "FAILED") {
		return
	}
	m.business.WithLabelValues(environment, status, failureClass).Inc()
	m.businessDuration.WithLabelValues(environment, status).Observe(seconds)
}

type response struct {
	http.ResponseWriter
	status int
}

func (w *response) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
		w.ResponseWriter.WriteHeader(status)
	}
}
func (w *response) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(200)
	}
	return w.ResponseWriter.Write(b)
}
func (w *response) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// Wrap uses matched route patterns, never raw paths or query values as labels.
func (m *Metrics) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/metrics" || r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
			next.ServeHTTP(w, r)
			return
		}
		started := time.Now()
		writer := &response{ResponseWriter: w}
		next.ServeHTTP(writer, r)
		status := writer.status
		if status == 0 {
			status = 200
		}
		route := r.Pattern
		if route == "" {
			route = "unmatched"
		}
		method := r.Method
		if !strings.Contains("|GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS|", "|"+method+"|") {
			method = "OTHER"
		}
		m.requests.WithLabelValues(method, route, strconv.Itoa(status)).Inc()
		m.duration.WithLabelValues(method, route).Observe(time.Since(started).Seconds())
	})
}
