package incident

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const testTraceID = "0123456789abcdef0123456789abcdef"

func TestJaegerSearchScopesAndProjectsTrace(t *testing.T) {
	start := time.Date(2026, 9, 27, 1, 0, 0, 0, time.UTC)
	end := start.Add(2 * time.Hour)
	started := start.Add(20 * time.Minute).UnixNano()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/trace-summaries":
			q := r.URL.Query()
			if q.Get("query.serviceName") != "payment-service" || q.Get("query.startTimeMin") != start.Format(time.RFC3339Nano) || q.Get("query.startTimeMax") != end.Format(time.RFC3339Nano) || q.Get("query.searchDepth") != "8" || q.Get("query.attributes") != "" {
				t.Errorf("unexpected trace search: %s", r.URL.String())
			}
			_, _ = io.WriteString(w, `{"summaries":[{"traceId":"`+testTraceID+`"}]}`)
		case "/api/v3/traces/" + testTraceID:
			_, _ = fmt.Fprintf(w, `{"result":{"resourceSpans":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"api-gateway"}}]},"scopeSpans":[{"spans":[{"traceId":"%s","spanId":"1111111111111111","startTimeUnixNano":"%d","endTimeUnixNano":"%d","attributes":[{"key":"deployment.environment.name","value":{"stringValue":"development"}}]}]}]},{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"payment-service"}}]},"scopeSpans":[{"spans":[{"traceId":"%s","spanId":"2222222222222222","parentSpanId":"1111111111111111","startTimeUnixNano":"%d","endTimeUnixNano":"%d","attributes":[{"key":"http.response.status_code","value":{"intValue":"503"}}]},{"traceId":"%s","spanId":"3333333333333333","parentSpanId":"2222222222222222","startTimeUnixNano":"%d","endTimeUnixNano":"%d","attributes":[{"key":"db.system.name","value":{"stringValue":"postgresql"}}],"status":{"code":"STATUS_CODE_ERROR"}}]}]}]}}`, testTraceID, started, started+int64(time.Second), testTraceID, started+int64(time.Millisecond), started+int64(500*time.Millisecond), testTraceID, started+int64(time.Millisecond), started+int64(250*time.Millisecond))
		default:
			t.Errorf("unexpected Jaeger request: %s", r.URL.String())
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	search, err := NewJaegerSearch(server.URL, true)
	if err != nil {
		t.Fatal(err)
	}
	items, err := search.SearchTraces(context.Background(), Incident{Service: "payment-service", Environment: "development"}, start, end)
	if err != nil || len(items) != 1 || len(items[0].Nodes) != 3 || len(items[0].Edges) != 2 {
		t.Fatalf("trace projection: %+v %v", items, err)
	}
	if !items[0].Nodes[1].Error || items[0].Nodes[1].Name != "payment-service" {
		t.Fatalf("payment error missing: %+v", items[0].Nodes)
	}
	other, err := search.SearchTraces(context.Background(), Incident{Service: "payment-service", Environment: "staging"}, start, end)
	if err != nil || len(other) != 0 {
		t.Fatalf("cross-environment trace leaked: %+v %v", other, err)
	}
	if _, err := search.SearchTraces(context.Background(), Incident{Service: `payment-service&query.searchDepth=10000`, Environment: "development"}, start, end); !errors.Is(err, ErrInvalid) {
		t.Fatal("accepted unsafe service name")
	}
}

func TestJaegerSearchFocusesPurchaseIncidentOnDetection(t *testing.T) {
	start := time.Date(2026, 9, 28, 0, 30, 0, 0, time.UTC)
	detected := start.Add(15 * time.Minute)
	end := start.Add(2 * time.Hour)
	started := detected.Add(-time.Minute).UnixNano()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/trace-summaries":
			q := r.URL.Query()
			if q.Get("query.serviceName") != "api-gateway" || q.Get("query.operationName") != "POST /api/v1/transactions" || q.Get("query.startTimeMin") != start.Format(time.RFC3339Nano) || q.Get("query.startTimeMax") != detected.Add(5*time.Minute).Format(time.RFC3339Nano) || q.Get("query.searchDepth") != "8" {
				t.Errorf("purchase trace search did not focus on detection: %s", r.URL.String())
			}
			_, _ = io.WriteString(w, `{"summaries":[{"traceId":"`+testTraceID+`"}]}`)
		case "/api/v3/traces/" + testTraceID:
			_, _ = fmt.Fprintf(w, `{"result":{"resourceSpans":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"api-gateway"}}]},"scopeSpans":[{"spans":[{"traceId":"%s","spanId":"1111111111111111","startTimeUnixNano":"%d","endTimeUnixNano":"%d","attributes":[{"key":"deployment.environment.name","value":{"stringValue":"development"}},{"key":"transaction.outcome","value":{"stringValue":"FAILED"}},{"key":"http.response.status_code","value":{"intValue":"200"}}]}]}]}]}}`, testTraceID, started, started+int64(time.Second))
		default:
			t.Errorf("unexpected Jaeger request: %s", r.URL.String())
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	search, err := NewJaegerSearch(server.URL, true)
	if err != nil {
		t.Fatal(err)
	}
	items, err := search.SearchTraces(context.Background(), Incident{Service: "api-gateway", Environment: "development", DetectedAt: detected}, start, end)
	if err != nil || len(items) != 1 || items[0].ID != testTraceID || len(items[0].Nodes) != 1 || !items[0].Nodes[0].Error {
		t.Fatalf("purchase trace projection: %+v %v", items, err)
	}
}

func TestJaegerSearchConfigAndFailures(t *testing.T) {
	for _, endpoint := range []string{"http://remote.example", "https://user:password@example.com", "https://example.com?token=x", "https://example.com/path"} {
		if _, err := NewJaegerSearch(endpoint, true); err == nil {
			t.Fatalf("accepted unsafe endpoint: %s", endpoint)
		}
	}
	for _, reply := range []struct {
		status int
		body   string
	}{{503, "private vendor detail"}, {200, "{}"}, {200, strings.Repeat("x", (128<<10)+1)}} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(reply.status)
			_, _ = io.WriteString(w, reply.body)
		}))
		search, _ := NewJaegerSearch(server.URL, true)
		_, err := search.SearchTraces(context.Background(), Incident{Service: "payment-service", Environment: "development"}, time.Now().Add(-time.Hour), time.Now())
		server.Close()
		if err == nil || strings.Contains(err.Error(), "private vendor detail") {
			t.Fatalf("unsafe trace failure: %v", err)
		}
	}
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://example.com/private", http.StatusFound)
	}))
	defer redirect.Close()
	search, _ := NewJaegerSearch(redirect.URL, true)
	if _, err := search.SearchTraces(context.Background(), Incident{Service: "payment-service", Environment: "development"}, time.Now().Add(-time.Hour), time.Now()); err == nil {
		t.Fatal("followed Jaeger redirect")
	}
}
