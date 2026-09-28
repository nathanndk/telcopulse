package incident

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDatadogMetricsScopesAndProjectsPodSeries(t *testing.T) {
	start := time.Date(2026, 9, 28, 1, 0, 0, 0, time.UTC)
	end := start.Add(2 * time.Hour)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v2/query/timeseries" || r.Header.Get("Authorization") != "Bearer test-datadog-token" || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("unexpected Datadog request: %s %s", r.Method, r.URL.String())
		}
		var body struct {
			Data struct {
				Type       string `json:"type"`
				Attributes struct {
					From    int64 `json:"from"`
					To      int64 `json:"to"`
					Queries []struct {
						Query string `json:"query"`
					} `json:"queries"`
				} `json:"attributes"`
			} `json:"data"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body.Data.Type != "timeseries_request" || body.Data.Attributes.From != start.UnixMilli() || body.Data.Attributes.To != end.UnixMilli() || len(body.Data.Attributes.Queries) != 3 {
			t.Errorf("wrong detection scope: %+v", body)
		}
		for _, query := range body.Data.Attributes.Queries {
			if !strings.Contains(query.Query, "{env:development,service:payment-service}") || !strings.Contains(query.Query, "by {env,service,kube_namespace,pod_name}") {
				t.Errorf("unscoped metric query: %s", query.Query)
			}
		}
		_, _ = io.WriteString(w, `{"data":{"type":"timeseries_response","attributes":{"series":[{"group_tags":["env:development","service:payment-service","kube_namespace:telcopulse-dev","pod_name:payment-abc"],"query_index":0},{"group_tags":["env:staging","service:payment-service","kube_namespace:telcopulse-staging","pod_name:payment-other"],"query_index":1},{"group_tags":["env:development","service:payment-service","kube_namespace:telcopulse-dev","pod_name:payment-abc"],"query_index":2}],"times":[`+jsonNumber(start.Add(10*time.Minute).UnixMilli())+`,`+jsonNumber(start.Add(20*time.Minute).UnixMilli())+`],"values":[[0,1],[50,60],[null,500000000]]}}}`)
	}))
	defer server.Close()
	client, err := NewDatadogMetrics(server.URL+"/api/v2/query/timeseries", "test-datadog-token", true)
	if err != nil {
		t.Fatal(err)
	}
	series, limited, err := client.SearchInfrastructure(context.Background(), Incident{Service: "payment-service", Environment: "development"}, start, end)
	if err != nil || limited || len(series) != 2 || series[0].Key != "restarts" || series[0].Pod != "payment-abc" || len(series[0].Points) != 2 || series[1].Key != "cpu" || len(series[1].Points) != 1 {
		t.Fatalf("incorrect projection: %+v limited=%v err=%v", series, limited, err)
	}
	if _, _, err := client.SearchInfrastructure(context.Background(), Incident{Service: `payment-service} by {host}`, Environment: "development"}, start, end); err == nil {
		t.Fatal("accepted unsafe incident service")
	}
}

func jsonNumber(value int64) string {
	data, _ := json.Marshal(value)
	return string(data)
}

func TestDatadogMetricsRejectsUnsafeConfigurationAndResponses(t *testing.T) {
	for _, endpoint := range []string{"http://vendor.example/api/v2/query/timeseries", "https://user:pass@example.com/api/v2/query/timeseries", "https://example.com/api/v1/query", "https://example.com/api/v2/query/timeseries?token=x"} {
		if _, err := NewDatadogMetrics(endpoint, "test-datadog-token", true); err == nil {
			t.Fatalf("accepted unsafe endpoint: %s", endpoint)
		}
	}
	start, end := time.Now().Add(-time.Hour), time.Now()
	for _, response := range []struct {
		status int
		body   string
	}{{503, "private vendor detail"}, {200, "{}"}, {200, strings.Repeat("x", 256<<10+1)}, {200, `{"errors":["private vendor detail"]}`}, {200, `{"data":{"type":"timeseries_response","attributes":{"series":[{"query_index":9}],"times":[],"values":[[]]}}}`}} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(response.status)
			_, _ = io.WriteString(w, response.body)
		}))
		client, _ := NewDatadogMetrics(server.URL+"/api/v2/query/timeseries", "test-datadog-token", true)
		_, _, err := client.SearchInfrastructure(context.Background(), Incident{Service: "payment-service", Environment: "development"}, start, end)
		server.Close()
		if err == nil || strings.Contains(err.Error(), "private vendor detail") {
			t.Fatalf("unsafe response accepted: %v", err)
		}
	}
}

func TestDatadogMetricsAcceptsEmptyErrorList(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"data":{"type":"timeseries_response","attributes":{"series":[],"times":[],"values":[]}},"errors":[]}`)
	}))
	defer server.Close()
	client, err := NewDatadogMetrics(server.URL+"/api/v2/query/timeseries", "test-datadog-token", true)
	if err != nil {
		t.Fatal(err)
	}
	end := time.Now()
	items, limited, err := client.SearchInfrastructure(context.Background(), Incident{Service: "payment-service", Environment: "development"}, end.Add(-time.Hour), end)
	if err != nil || limited || len(items) != 0 {
		t.Fatalf("empty successful response: %+v limited=%v err=%v", items, limited, err)
	}
}
