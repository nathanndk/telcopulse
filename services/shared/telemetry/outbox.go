package telemetry

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
)

type outboxCollector struct {
	pool             *pgxpool.Pool
	table            string
	pending, age, up *prometheus.Desc
}

func newOutboxCollector(pool *pgxpool.Pool, service string) *outboxCollector {
	table := map[string]string{"api-gateway": "public.event_outbox", "payment-service": "payment.event_outbox", "notification-service": "notification.event_outbox"}[service]
	if table == "" {
		return nil
	}
	return &outboxCollector{pool: pool, table: table, pending: prometheus.NewDesc("event_outbox_pending", "Unpublished events in this service outbox.", nil, nil), age: prometheus.NewDesc("event_outbox_oldest_age_seconds", "Age of the oldest unpublished event, zero when empty.", nil, nil), up: prometheus.NewDesc("event_outbox_collection_success", "Whether the outbox query succeeded.", nil, nil)}
}
func (c *outboxCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.pending
	ch <- c.age
	ch <- c.up
}
func (c *outboxCollector) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	var count, age float64
	err := c.pool.QueryRow(ctx, "SELECT count(*),coalesce(extract(epoch from now()-min(created_at)),0) FROM "+c.table+" WHERE published_at IS NULL").Scan(&count, &age)
	if err != nil {
		ch <- prometheus.MustNewConstMetric(c.up, prometheus.GaugeValue, 0)
		return
	}
	ch <- prometheus.MustNewConstMetric(c.up, prometheus.GaugeValue, 1)
	ch <- prometheus.MustNewConstMetric(c.pending, prometheus.GaugeValue, count)
	ch <- prometheus.MustNewConstMetric(c.age, prometheus.GaugeValue, age)
}
