package incident

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"telcopulse/services/shared/domain"
)

type fakeMetricSearcher func(context.Context, Incident, time.Time, time.Time) ([]MetricSeries, int, error)

func (f fakeMetricSearcher) SearchMetrics(ctx context.Context, i Incident, start, end time.Time) ([]MetricSeries, int, error) {
	return f(ctx, i, start, end)
}

func TestIncidentMetricEndpointUsesPersistedScope(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL required for metric endpoint integration")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	key, err := domain.NewID(12)
	if err != nil {
		t.Fatal(err)
	}
	created, _, err := (Store{Pool: pool}).Create(ctx, Create{Fields: Fields{Title: "Metric history integration", Severity: "SEV-3"}, Environment: "development", Service: "payment-service"}, "metric-search-"+key, "integration-test")
	if err != nil {
		t.Fatal(err)
	}
	called := false
	searcher := fakeMetricSearcher(func(_ context.Context, i Incident, start, end time.Time) ([]MetricSeries, int, error) {
		called = true
		if i.ID != created.ID || i.Environment != "development" || i.Service != "payment-service" || !start.Equal(created.DetectedAt.Add(-15*time.Minute)) || end.Sub(created.DetectedAt) > 2*time.Hour {
			t.Errorf("unsafe metric scope: %+v %s %s", i, start, end)
		}
		return []MetricSeries{{Key: "http_rps", Label: "HTTP requests per second", Unit: "requests/s", Scope: "shared-runtime", Points: []MetricPoint{}}}, 30, nil
	})
	log := slog.New(slog.NewJSONHandler(io.Discard, nil))
	handler := HandlerWithSources(pool, log, "test-mutation-token", nil, nil, searcher)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/internal/incidents/"+created.ID+"/metrics?window=2h&query=unsafe", nil))
	if w.Code != 200 || !called || !strings.Contains(w.Body.String(), `"key":"http_rps"`) {
		t.Fatalf("configured metric endpoint: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	HandlerWithSources(pool, log, "test-mutation-token", nil, nil, nil).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/internal/incidents/"+created.ID+"/metrics", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"configured":false`) {
		t.Fatalf("unconfigured metric endpoint: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/internal/incidents/"+created.ID+"/metrics?window=forever", nil))
	if w.Code != 422 {
		t.Fatalf("accepted invalid window: %d %s", w.Code, w.Body.String())
	}
}

func TestPrometheusRangeFixedQueriesAndSparseSamples(t *testing.T) {
	start := time.Date(2026, 9, 27, 1, 0, 0, 0, time.UTC)
	end := start.Add(2 * time.Hour)
	var mu sync.Mutex
	queries := map[string]bool{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/query_range" || q.Get("start") != start.Format(time.RFC3339Nano) || q.Get("end") != end.Format(time.RFC3339Nano) || q.Get("step") != "60s" {
			t.Errorf("unexpected range request: %s", r.URL.String())
		}
		expression := q.Get("query")
		if strings.Contains(expression, "secret") || strings.Contains(expression, "staging") || (!strings.Contains(expression, `environment="development"`) && !strings.Contains(expression, `service="payment-service"`)) {
			t.Errorf("unsafe query: %s", expression)
		}
		mu.Lock()
		queries[expression] = true
		mu.Unlock()
		if strings.Contains(expression, `status="SUCCESS"`) {
			_, _ = fmt.Fprintf(w, `{"status":"success","data":{"resultType":"matrix","result":[{"values":[[%d,"0.95"],[%d,"NaN"]]}]}}`, start.Add(time.Minute).Unix(), start.Add(2*time.Minute).Unix())
		} else {
			_, _ = io.WriteString(w, `{"status":"success","data":{"resultType":"matrix","result":[]}}`)
		}
	}))
	defer server.Close()
	client, err := NewPrometheusRange(server.URL, true)
	if err != nil {
		t.Fatal(err)
	}
	series, step, err := client.SearchMetrics(context.Background(), Incident{Service: "payment-service", Environment: "development"}, start, end)
	if err != nil || step != 60 || len(series) != 5 || len(queries) != 5 || len(series[0].Points) != 1 || series[0].Points[0].Value != 0.95 || len(series[1].Points) != 0 {
		t.Fatalf("range query: step=%d series=%+v queries=%d err=%v", step, series, len(queries), err)
	}
	if _, _, err := client.SearchMetrics(context.Background(), Incident{Service: `payment-service"} or vector(1)`, Environment: "development"}, start, end); !errors.Is(err, ErrInvalid) {
		t.Fatal("accepted arbitrary PromQL")
	}
}

func TestPrometheusRangeConfigurationAndFailures(t *testing.T) {
	for _, endpoint := range []string{"http://remote.example", "https://user:password@example.com", "https://example.com?token=x", "https://example.com/path"} {
		if _, err := NewPrometheusRange(endpoint, true); err == nil {
			t.Fatalf("accepted unsafe endpoint: %s", endpoint)
		}
	}
	for _, reply := range []struct {
		status int
		body   string
	}{
		{503, "private source detail"},
		{200, `{"status":"success","data":{"resultType":"vector","result":[]}}`},
		{200, `{"status":"success","data":{"resultType":"matrix","result":[{},{}]}}`},
		{200, strings.Repeat("x", (128<<10)+1)},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(reply.status)
			_, _ = io.WriteString(w, reply.body)
		}))
		client, _ := NewPrometheusRange(server.URL, true)
		_, _, err := client.SearchMetrics(context.Background(), Incident{Service: "payment-service", Environment: "development"}, time.Now().Add(-time.Hour), time.Now())
		server.Close()
		if err == nil || strings.Contains(err.Error(), "private source detail") {
			t.Fatalf("unsafe metric failure: %v", err)
		}
	}
}
