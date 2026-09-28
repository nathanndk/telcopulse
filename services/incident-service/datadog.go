package incident

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"slices"
	"sort"
	"strings"
	"time"
)

type InfrastructurePoint struct {
	At    time.Time `json:"at"`
	Value float64   `json:"value"`
}

type InfrastructureSeries struct {
	Key       string                `json:"key"`
	Label     string                `json:"label"`
	Unit      string                `json:"unit"`
	Namespace string                `json:"namespace"`
	Pod       string                `json:"pod"`
	Points    []InfrastructurePoint `json:"points"`
}

type InfrastructureEvidence struct {
	Configured bool                   `json:"configured"`
	Source     string                 `json:"source"`
	Start      time.Time              `json:"window_start"`
	End        time.Time              `json:"window_end"`
	Limited    bool                   `json:"limited"`
	Series     []InfrastructureSeries `json:"series"`
}

type InfrastructureSearcher interface {
	SearchInfrastructure(context.Context, Incident, time.Time, time.Time) ([]InfrastructureSeries, bool, error)
}

type DatadogMetrics struct {
	endpoint string
	token    string
	client   *http.Client
}

var infrastructureMetrics = []struct{ key, metric, label, unit, aggregate string }{
	{"restarts", "kubernetes.containers.restarts", "Container restarts", "count", "max"},
	{"memory", "kubernetes.memory.working_set", "Memory working set", "bytes", "avg"},
	{"cpu", "kubernetes.cpu.usage.total", "CPU usage", "nanocores", "avg"},
}

func NewDatadogMetrics(endpoint, token string, local bool) (*DatadogMetrics, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "/api/v2/query/timeseries" {
		return nil, errors.New("invalid Datadog metrics endpoint")
	}
	loopback := slices.Contains([]string{"localhost", "127.0.0.1", "::1"}, u.Hostname())
	if u.Scheme != "https" && (!local || !loopback || u.Scheme != "http") {
		return nil, errors.New("Datadog metrics requires HTTPS")
	}
	if len(token) < 16 || len(token) > 4096 || strings.ContainsAny(token, "\r\n") {
		return nil, errors.New("invalid Datadog metrics credential")
	}
	return &DatadogMetrics{endpoint: endpoint, token: token, client: &http.Client{
		Timeout: 8 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("redirect disabled")
		},
	}}, nil
}

func DatadogMetricsFromEnv() (InfrastructureSearcher, error) {
	endpoint, path := os.Getenv("DATADOG_METRICS_URL"), os.Getenv("DATADOG_TOKEN_FILE")
	if endpoint == "" && path == "" {
		return nil, nil
	}
	if endpoint == "" || path == "" {
		return nil, errors.New("Datadog metrics URL and token file must be configured together")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("cannot read Datadog metrics token file")
	}
	data, readErr := io.ReadAll(io.LimitReader(file, 4097))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil || len(data) > 4096 {
		return nil, errors.New("invalid Datadog metrics token file")
	}
	return NewDatadogMetrics(endpoint, strings.TrimSpace(string(data)), os.Getenv("APP_MODE") == "local")
}

func (d *DatadogMetrics) SearchInfrastructure(ctx context.Context, incident Incident, start, end time.Time) ([]InfrastructureSeries, bool, error) {
	if !splunkIdentifier.MatchString(incident.Service) || !slices.Contains([]string{"development", "staging"}, incident.Environment) || !end.After(start) || end.Sub(start) > 73*time.Hour {
		return nil, false, invalid(errors.New("invalid infrastructure search scope"))
	}
	interval := int64(math.Ceil(float64(end.Sub(start).Milliseconds()) / 120.0))
	if interval < 60_000 {
		interval = 60_000
	}
	queries, formulas := make([]map[string]string, 0, len(infrastructureMetrics)), make([]map[string]string, 0, len(infrastructureMetrics))
	for index, metric := range infrastructureMetrics {
		name := string(rune('a' + index))
		query := metric.aggregate + ":" + metric.metric + "{env:" + incident.Environment + ",service:" + incident.Service + "} by {env,service,kube_namespace,pod_name}"
		queries = append(queries, map[string]string{"data_source": "metrics", "query": query, "name": name})
		formulas = append(formulas, map[string]string{"formula": name})
	}
	body, err := json.Marshal(map[string]any{"data": map[string]any{"type": "timeseries_request", "attributes": map[string]any{
		"from": start.UnixMilli(), "to": end.UnixMilli(), "interval": interval, "queries": queries, "formulas": formulas,
	}}})
	if err != nil {
		return nil, false, errors.New("invalid infrastructure query")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, false, errors.New("invalid infrastructure query")
	}
	req.Header.Set("Authorization", "Bearer "+d.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := d.client.Do(req)
	if err != nil {
		return nil, false, errors.New("Datadog metrics unavailable")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, false, errors.New("Datadog metrics rejected query")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, (256<<10)+1))
	if err != nil || len(data) > 256<<10 {
		return nil, false, errors.New("Datadog metrics response exceeds limit")
	}
	var payload struct {
		Data struct {
			Type       string `json:"type"`
			Attributes struct {
				Series []struct {
					Tags       []string `json:"group_tags"`
					QueryIndex int      `json:"query_index"`
				} `json:"series"`
				Times  []int64      `json:"times"`
				Values [][]*float64 `json:"values"`
			} `json:"attributes"`
		} `json:"data"`
		Errors json.RawMessage `json:"errors"`
	}
	if json.Unmarshal(data, &payload) != nil {
		return nil, false, errors.New("invalid Datadog metrics response")
	}
	errText := strings.TrimSpace(string(payload.Errors))
	if payload.Data.Type != "timeseries_response" || (errText != "" && errText != "null" && errText != "[]" && errText != `""`) || payload.Data.Attributes.Series == nil || len(payload.Data.Attributes.Series) != len(payload.Data.Attributes.Values) || len(payload.Data.Attributes.Series) > 48 || len(payload.Data.Attributes.Times) > 240 {
		return nil, false, errors.New("invalid Datadog metrics response")
	}
	result := make([]InfrastructureSeries, 0, 12)
	counts := make([]int, len(infrastructureMetrics))
	limited := false
	for index, series := range payload.Data.Attributes.Series {
		if series.QueryIndex < 0 || series.QueryIndex >= len(infrastructureMetrics) || len(payload.Data.Attributes.Values[index]) != len(payload.Data.Attributes.Times) {
			return nil, false, errors.New("invalid Datadog metrics response")
		}
		tags := map[string]string{}
		for _, tag := range series.Tags {
			key, value, ok := strings.Cut(tag, ":")
			if ok && len(value) <= 120 && splunkIdentifier.MatchString(key) {
				tags[key] = value
			}
		}
		if tags["env"] != incident.Environment || tags["service"] != incident.Service || tags["kube_namespace"] == "" || tags["pod_name"] == "" {
			continue
		}
		if counts[series.QueryIndex] == 4 {
			limited = true
			continue
		}
		metric := infrastructureMetrics[series.QueryIndex]
		item := InfrastructureSeries{Key: metric.key, Label: metric.label, Unit: metric.unit, Namespace: tags["kube_namespace"], Pod: tags["pod_name"], Points: []InfrastructurePoint{}}
		for point, at := range payload.Data.Attributes.Times {
			value := payload.Data.Attributes.Values[index][point]
			if value == nil || math.IsNaN(*value) || math.IsInf(*value, 0) || *value < 0 || at < start.UnixMilli() || at > end.UnixMilli() {
				continue
			}
			item.Points = append(item.Points, InfrastructurePoint{At: time.UnixMilli(at).UTC(), Value: *value})
		}
		sort.Slice(item.Points, func(a, b int) bool { return item.Points[a].At.Before(item.Points[b].At) })
		result = append(result, item)
		counts[series.QueryIndex]++
	}
	return result, limited, nil
}
