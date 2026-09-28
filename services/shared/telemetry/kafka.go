package telemetry

import (
	"context"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
	"telcopulse/services/shared/events"
)

type kafkaCollector struct {
	client  *kadm.Client
	lag, up *prometheus.Desc
}

// Kafka registers broker-derived committed-offset lag for the notification group.
// Failed or missing measurements are omitted rather than reported as zero.
func (m *Metrics) Kafka(client *kgo.Client) {
	m.Registry.MustRegister(&kafkaCollector{client: kadm.NewClient(client), lag: prometheus.NewDesc("kafka_consumer_lag", "Broker end offset minus committed consumer offset.", []string{"group", "topic", "partition"}, nil), up: prometheus.NewDesc("kafka_lag_collection_success", "Whether all notification lag measurements were available.", nil, nil)})
}
func (c *kafkaCollector) Describe(ch chan<- *prometheus.Desc) { ch <- c.lag; ch <- c.up }
func (c *kafkaCollector) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	const group = "telcopulse-notifications-v1"
	groups, err := c.client.Lag(ctx, group)
	entry, ok := groups[group]
	if err != nil || !ok || entry.Error() != nil || len(entry.Lag[events.PurchaseTopic]) == 0 {
		ch <- prometheus.MustNewConstMetric(c.up, prometheus.GaugeValue, 0)
		return
	}
	metrics := []prometheus.Metric{}
	for _, part := range entry.Lag[events.PurchaseTopic] {
		if part.Err != nil || part.Lag < 0 {
			ch <- prometheus.MustNewConstMetric(c.up, prometheus.GaugeValue, 0)
			return
		}
		metrics = append(metrics, prometheus.MustNewConstMetric(c.lag, prometheus.GaugeValue, float64(part.Lag), group, events.PurchaseTopic, strconv.Itoa(int(part.Partition))))
	}
	ch <- prometheus.MustNewConstMetric(c.up, prometheus.GaugeValue, 1)
	for _, metric := range metrics {
		ch <- metric
	}
}
