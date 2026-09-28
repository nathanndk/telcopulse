// Package servicehealth reads fixed operational measurements from Prometheus.
package servicehealth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Client is a bounded read-only adapter; callers cannot supply PromQL or URLs.
type Client struct {
	endpoint string
	http     *http.Client
}

// Service reports shared-runtime HTTP health, not customer business outcomes.
type Service struct {
	ID        string   `json:"id"`
	Status    string   `json:"status"`
	Reason    string   `json:"reason"`
	Up        *float64 `json:"up"`
	RPS       *float64 `json:"rps"`
	ErrorRate *float64 `json:"error_rate"`
	P95MS     *float64 `json:"p95_ms"`
}

// Snapshot contains measurements evaluated at one instant over a five-minute window.
type Snapshot struct {
	Scope         string    `json:"scope"`
	ObservedAt    time.Time `json:"observed_at"`
	WindowSeconds int       `json:"window_seconds"`
	Items         []Service `json:"items"`
}

// New validates trusted server configuration. Empty configuration disables telemetry.
func New(endpoint string) (*Client, error) {
	if endpoint == "" {
		return nil, nil
	}
	u, err := url.Parse(endpoint)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("invalid Prometheus URL")
	}
	return &Client{endpoint: strings.TrimRight(endpoint, "/") + "/api/v1/query", http: &http.Client{Timeout: 2 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

var queries = []string{
	`min by (service) (up{job="telcopulse"} and (time() - timestamp(up{job="telcopulse"}) < 30))`,
	`sum by (service) (rate(http_requests_total{job="telcopulse"}[5m]))`,
	`(sum by (service) (rate(http_requests_total{job="telcopulse",status=~"5.."}[5m])) or 0 * sum by (service) (rate(http_requests_total{job="telcopulse"}[5m]))) / sum by (service) (rate(http_requests_total{job="telcopulse"}[5m]))`,
	`1000 * histogram_quantile(0.95, sum by (service, le) (rate(http_request_duration_seconds_bucket{job="telcopulse"}[5m])))`,
}

// Read fails closed on partial query failures and preserves absent samples as null.
func (c *Client) Read(ctx context.Context) (Snapshot, error) {
	if c == nil {
		return Snapshot{}, errors.New("service telemetry is not configured")
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	now := time.Now().UTC()
	values := make([]map[string]*float64, len(queries))
	for i, query := range queries {
		v, err := c.query(ctx, query, now)
		if err != nil {
			return Snapshot{}, fmt.Errorf("service telemetry: %w", err)
		}
		values[i] = v
	}
	out := Snapshot{Scope: "shared-runtime", ObservedAt: now, WindowSeconds: 300, Items: []Service{}}
	for _, id := range []string{"auth-service", "api-gateway", "subscriber-service", "package-service", "payment-service", "notification-service", "incident-service"} {
		s := Service{ID: id, Up: values[0][id], RPS: values[1][id], ErrorRate: values[2][id], P95MS: values[3][id]}
		s.Status, s.Reason = classify(s)
		out.Items = append(out.Items, s)
	}
	return out, nil
}

func classify(s Service) (string, string) {
	if s.Up == nil {
		return "Unknown", "No recent scrape measurement"
	}
	if *s.Up == 0 {
		return "Critical", "Prometheus scrape failed"
	}
	if *s.Up != 1 {
		return "Unknown", "Invalid scrape measurement"
	}
	if s.RPS == nil || *s.RPS <= 0 {
		return "Unknown", "Scrape reachable; no observed HTTP traffic"
	}
	if s.ErrorRate == nil || *s.ErrorRate > 1 || s.P95MS == nil {
		return "Unknown", "Incomplete HTTP measurements"
	}
	if *s.ErrorRate >= .05 {
		return "Critical", "HTTP server errors at least 5%"
	}
	if *s.ErrorRate >= .01 || *s.P95MS >= 1000 {
		return "Degraded", "HTTP errors at least 1% or P95 at least 1 second"
	}
	return "Healthy", "Recent scrape and HTTP measurements within thresholds"
}

func (c *Client) query(ctx context.Context, query string, at time.Time) (map[string]*float64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint+"?"+url.Values{"query": {query}, "time": {at.Format(time.RFC3339Nano)}, "timeout": {"1s"}}.Encode(), nil)
	if err != nil {
		return nil, err
	}
	response, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }() // ReadAll reports response read failures.
	if response.StatusCode != 200 {
		return nil, errors.New("query unavailable")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(body) > 1<<20 {
		return nil, errors.New("query response too large")
	}
	var result struct {
		Status   string   `json:"status"`
		Warnings []string `json:"warnings"`
		Data     struct {
			Type   string `json:"resultType"`
			Result []struct {
				Metric map[string]string `json:"metric"`
				Value  []json.RawMessage `json:"value"`
			} `json:"result"`
		} `json:"data"`
	}
	if err = json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	if result.Status != "success" || result.Data.Type != "vector" || result.Data.Result == nil || len(result.Data.Result) > 100 || len(result.Warnings) > 0 {
		return nil, errors.New("incomplete query result")
	}
	out := map[string]*float64{}
	for _, sample := range result.Data.Result {
		if len(sample.Value) != 2 || sample.Metric["service"] == "" {
			return nil, errors.New("invalid query sample")
		}
		id := sample.Metric["service"]
		if _, exists := out[id]; exists {
			return nil, errors.New("duplicate service sample")
		}
		var timestamp float64
		var raw string
		if json.Unmarshal(sample.Value[0], &timestamp) != nil || json.Unmarshal(sample.Value[1], &raw) != nil {
			return nil, errors.New("invalid query value")
		}
		if math.Abs(timestamp-float64(at.UnixNano())/1e9) > 30 {
			return nil, errors.New("stale query sample")
		}
		value, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return nil, errors.New("invalid numeric sample")
		}
		out[id] = nil
		if !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0 {
			out[id] = &value
		}
	}
	return out, nil
}
