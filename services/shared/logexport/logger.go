package logexport

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// LoggerFromEnv preserves stdout and enables optional, best-effort HEC export.
func LoggerFromEnv(service string) (*slog.Logger, *Queue, error) {
	var writer io.Writer = os.Stdout
	var queue *Queue
	endpoint, token := os.Getenv("SPLUNK_HEC_URL"), os.Getenv("SPLUNK_HEC_TOKEN")
	if path := os.Getenv("SPLUNK_HEC_TOKEN_FILE"); path != "" {
		if token != "" {
			return nil, nil, errors.New("configure only one HEC token source")
		}
		file, err := os.Open(path)
		if err != nil {
			return nil, nil, errors.New("cannot read HEC token file")
		}
		data, readErr := io.ReadAll(io.LimitReader(file, 4097))
		closeErr := file.Close()
		if readErr != nil || closeErr != nil || len(data) > 4096 {
			return nil, nil, errors.New("invalid HEC token file")
		}
		token = strings.TrimSpace(string(data))
		if token == "" {
			return nil, nil, errors.New("empty HEC token file")
		}
	}
	if endpoint != "" || token != "" {
		if endpoint == "" || token == "" {
			return nil, nil, errors.New("SPLUNK_HEC_URL and SPLUNK_HEC_TOKEN must be configured together")
		}
		hec, err := NewHEC(endpoint, token, os.Getenv("SPLUNK_HEC_INDEX"), os.Getenv("APP_MODE") == "local")
		if err != nil {
			return nil, nil, err
		}
		queue = NewQueue(hec)
		writer = io.MultiWriter(os.Stdout, queue)
	}
	return slog.New(slog.NewJSONHandler(writer, nil)).With("service", service, "version", "0.2.0"), queue, nil
}

// Shutdown gives accepted records five seconds to drain before cancellation.
func Shutdown(q *Queue) {
	if q == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if q.Close(ctx) != nil {
		_, _ = io.WriteString(os.Stderr, "log export drain deadline exceeded\n")
	}
}

// Register exposes bounded delivery outcomes without receiver or token labels.
func Register(registry *prometheus.Registry, q *Queue) {
	if q == nil {
		return
	}
	for _, outcome := range []string{"accepted", "failed", "dropped"} {
		registry.MustRegister(prometheus.NewCounterFunc(prometheus.CounterOpts{Name: "log_export_events_total", Help: "Best-effort log export outcomes; accepted does not mean indexed.", ConstLabels: prometheus.Labels{"outcome": outcome}}, func() float64 {
			s := q.Stats()
			switch outcome {
			case "accepted":
				return float64(s.Accepted)
			case "failed":
				return float64(s.Failed)
			default:
				return float64(s.Dropped)
			}
		}))
	}
	registry.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "log_export_pending", Help: "Buffered log records awaiting export."}, func() float64 { return float64(q.Stats().Pending) }))
}
