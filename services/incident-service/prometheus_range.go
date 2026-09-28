package incident

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

type MetricPoint struct {
	At    time.Time `json:"at"`
	Value float64   `json:"value"`
}

type MetricSeries struct {
	Key    string        `json:"key"`
	Label  string        `json:"label"`
	Unit   string        `json:"unit"`
	Scope  string        `json:"scope"`
	Points []MetricPoint `json:"points"`
}

type MetricEvidence struct {
	Configured bool           `json:"configured"`
	Source     string         `json:"source"`
	Start      time.Time      `json:"window_start"`
	End        time.Time      `json:"window_end"`
	Step       int            `json:"step_seconds"`
	Series     []MetricSeries `json:"series"`
}

type MetricSearcher interface {
	SearchMetrics(context.Context, Incident, time.Time, time.Time) ([]MetricSeries, int, error)
}

type PrometheusRange struct {
	base   string
	client *http.Client
}

func NewPrometheusRange(base string, local bool) (*PrometheusRange, error) {
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, errors.New("invalid Prometheus metrics URL")
	}
	internal := u.Hostname() == "prometheus" || u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1"
	if u.Scheme != "https" && (!local || !internal || u.Scheme != "http") {
		return nil, errors.New("Prometheus metrics requires HTTPS")
	}
	return &PrometheusRange{base: strings.TrimRight(base, "/"), client: &http.Client{
		Timeout:       7 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("redirect disabled") },
	}}, nil
}

func PrometheusRangeFromEnv() (MetricSearcher, error) {
	if base := os.Getenv("PROMETHEUS_URL"); base != "" {
		return NewPrometheusRange(base, os.Getenv("APP_MODE") == "local")
	}
	return nil, nil
}

type metricQuery struct {
	key, label, unit, scope, expression string
}

func incidentMetricQueries(i Incident) []metricQuery {
	env := i.Environment
	service := i.Service
	// The two business series have an environment label. The HTTP series are
	// process-wide and intentionally do not pretend to separate environments.
	return []metricQuery{
		{"business_success", "Business success", "fraction", "environment", `(sum(rate(transaction_total{environment="` + env + `",status="SUCCESS"}[5m])) or vector(0)) / sum(rate(transaction_total{environment="` + env + `"}[5m]))`},
		{"business_tpm", "Transactions per minute", "transactions/min", "environment", `sum(rate(transaction_total{environment="` + env + `"}[5m])) * 60`},
		{"http_rps", "HTTP requests per second", "requests/s", "shared-runtime", `sum(rate(http_requests_total{service="` + service + `"}[5m]))`},
		{"http_error", "HTTP 5xx fraction", "fraction", "shared-runtime", `(sum(rate(http_requests_total{service="` + service + `",status=~"5.."}[5m])) or vector(0)) / sum(rate(http_requests_total{service="` + service + `"}[5m]))`},
		{"http_p95", "HTTP P95 latency", "ms", "shared-runtime", `histogram_quantile(0.95, sum by (le) (rate(http_request_duration_seconds_bucket{service="` + service + `"}[5m]))) * 1000`},
	}
}

func (s *PrometheusRange) SearchMetrics(ctx context.Context, incident Incident, start, end time.Time) ([]MetricSeries, int, error) {
	if !splunkIdentifier.MatchString(incident.Service) || (incident.Environment != "development" && incident.Environment != "staging") || !end.After(start) || end.Sub(start) > 73*time.Hour {
		return nil, 0, invalid(errors.New("invalid metric search scope"))
	}
	step := int(math.Ceil(end.Sub(start).Seconds() / 120))
	if step < 30 {
		step = 30
	}
	queries := incidentMetricQueries(incident)
	series := make([]MetricSeries, len(queries))
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	errorsByIndex := make([]error, len(queries))
	for index, query := range queries {
		wg.Add(1)
		go func() {
			defer wg.Done()
			points, err := s.query(ctx, query.expression, start, end, step)
			errorsByIndex[index] = err
			series[index] = MetricSeries{Key: query.key, Label: query.label, Unit: query.unit, Scope: query.scope, Points: points}
		}()
	}
	wg.Wait()
	for _, err := range errorsByIndex {
		if err != nil {
			return nil, 0, err
		}
	}
	return series, step, nil
}

func (s *PrometheusRange) query(ctx context.Context, expression string, start, end time.Time, step int) ([]MetricPoint, error) {
	q := url.Values{
		"query": {expression},
		"start": {start.UTC().Format(time.RFC3339Nano)},
		"end":   {end.UTC().Format(time.RFC3339Nano)},
		"step":  {strconv.Itoa(step) + "s"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.base+"/api/v1/query_range?"+q.Encode(), nil)
	if err != nil {
		return nil, errors.New("invalid metric query")
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, errors.New("metric source unavailable")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("metric source rejected query")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, (128<<10)+1))
	if err != nil || len(data) > 128<<10 {
		return nil, errors.New("metric response exceeds limit")
	}
	var payload struct {
		Status string `json:"status"`
		Data   struct {
			Type   string `json:"resultType"`
			Result []struct {
				Values [][]json.RawMessage `json:"values"`
			} `json:"result"`
		} `json:"data"`
	}
	if json.Unmarshal(data, &payload) != nil || payload.Status != "success" || payload.Data.Type != "matrix" || payload.Data.Result == nil || len(payload.Data.Result) > 1 {
		return nil, errors.New("invalid metric response")
	}
	points := []MetricPoint{}
	if len(payload.Data.Result) == 0 {
		return points, nil
	}
	values := payload.Data.Result[0].Values
	if len(values) > 150 {
		return nil, errors.New("metric sample limit exceeded")
	}
	for _, pair := range values {
		if len(pair) != 2 {
			return nil, errors.New("invalid metric sample")
		}
		var timestamp float64
		var raw string
		if json.Unmarshal(pair[0], &timestamp) != nil || json.Unmarshal(pair[1], &raw) != nil || math.IsNaN(timestamp) || math.IsInf(timestamp, 0) {
			return nil, errors.New("invalid metric sample")
		}
		value, parseErr := strconv.ParseFloat(raw, 64)
		if parseErr != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
			continue
		}
		at := time.Unix(0, int64(timestamp*1e9)).UTC()
		if at.Before(start.Add(-time.Second)) || at.After(end.Add(time.Second)) {
			return nil, errors.New("metric sample outside window")
		}
		points = append(points, MetricPoint{At: at, Value: value})
	}
	return points, nil
}
