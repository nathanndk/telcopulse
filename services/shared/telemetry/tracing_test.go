package telemetry_test

import (
	"bytes"
	"context"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"telcopulse/services/shared/rpc"
	"telcopulse/services/shared/telemetry"
	"testing"
)

func TestHTTPExportsConnectedSpansAndPrivateLogs(t *testing.T) {
	var mu sync.Mutex
	var spans []*tracepb.Span
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/traces" {
			t.Errorf("OTLP path %s", r.URL.Path)
		}
		payload, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		var request collector.ExportTraceServiceRequest
		if err := proto.Unmarshal(payload, &request); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		encoded, err := protojson.Marshal(&request)
		if err != nil {
			t.Error(err)
		}
		for _, secret := range []string{"private-id", "subscriber-secret", "token-secret", "payload-secret"} {
			if bytes.Contains(encoded, []byte(secret)) {
				t.Errorf("export leaked %s", secret)
			}
		}
		mu.Lock()
		for _, resource := range request.ResourceSpans {
			for _, scope := range resource.ScopeSpans {
				spans = append(spans, scope.Spans...)
			}
		}
		mu.Unlock()
		w.Header().Set("Content-Type", "application/x-protobuf")
		w.WriteHeader(200)
	}))
	defer sink.Close()
	provider, err := telemetry.Tracing(context.Background(), "integration-test", sink.URL)
	if err != nil {
		t.Fatal(err)
	}
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	defer otel.SetTracerProvider(previous)
	defer func() {
		if err := provider.Shutdown(context.Background()); err != nil {
			t.Error(err)
		}
	}()
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	remoteMux := http.NewServeMux()
	remoteMux.HandleFunc("POST /internal/customers/{id}", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("traceparent") == "" {
			t.Error("trace context missing")
		}
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(`{"ok":true}`)); err != nil {
			t.Error(err)
		}
	})
	remote := httptest.NewServer(telemetry.TraceHTTP(remoteMux, logger))
	defer remote.Close()
	client := rpc.New(remote.URL, "token-secret")
	gateway := http.NewServeMux()
	gateway.HandleFunc("POST /purchase", func(w http.ResponseWriter, r *http.Request) {
		var out map[string]bool
		err := client.Call(r.Context(), "POST", "/internal/customers/private-id?msisdn=subscriber-secret", map[string]string{"data": "payload-secret"}, &out, "", "transaction-id")
		if err != nil {
			t.Error(err)
		}
		w.WriteHeader(201)
	})
	request := httptest.NewRequest("POST", "/purchase?msisdn=subscriber-secret", nil)
	request.Header.Set("traceparent", "00-0123456789abcdef0123456789abcdef-0123456789abcdef-01")
	telemetry.TraceHTTP(gateway, logger).ServeHTTP(httptest.NewRecorder(), request)
	if err := provider.ForceFlush(context.Background()); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(spans) != 3 {
		t.Fatalf("want three spans, got %d", len(spans))
	}
	byName := map[string]*tracepb.Span{}
	for _, span := range spans {
		byName[span.Name] = span
	}
	root, clientSpan, server := byName["POST /purchase"], byName["internal HTTP"], byName["POST /internal/customers/{id}"]
	if root == nil || clientSpan == nil || server == nil {
		t.Fatalf("missing spans: %v", byName)
	}
	if !bytes.Equal(clientSpan.ParentSpanId, root.SpanId) || !bytes.Equal(server.ParentSpanId, clientSpan.SpanId) {
		t.Fatal("broken parent-child chain")
	}
	if !bytes.Equal(root.TraceId, server.TraceId) || !bytes.Equal(root.TraceId, clientSpan.TraceId) {
		t.Fatal("trace IDs differ")
	}
	if root.Kind != tracepb.Span_SPAN_KIND_SERVER || clientSpan.Kind != tracepb.Span_SPAN_KIND_CLIENT || server.Kind != tracepb.Span_SPAN_KIND_SERVER {
		t.Fatal("wrong span kinds")
	}
	if !strings.Contains(logs.String(), `"trace_id":"0123456789abcdef0123456789abcdef"`) || !strings.Contains(logs.String(), `"span_id":`) {
		t.Fatal("logs lack trace correlation")
	}
	for _, secret := range []string{"private-id", "subscriber-secret", "token-secret", "payload-secret"} {
		if strings.Contains(logs.String(), secret) {
			t.Fatalf("logs leaked %s", secret)
		}
	}
}

func TestHTTPErrorAndProbeTracing(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	defer otel.SetTracerProvider(previous)
	defer func() {
		if err := provider.Shutdown(context.Background()); err != nil {
			t.Error(err)
		}
	}()
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	handler := telemetry.TraceHTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }), logger)
	for _, path := range []string{"/healthz", "/readyz", "/metrics"} {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", path, nil))
	}
	if len(exporter.GetSpans()) != 0 {
		t.Fatal("probes created spans")
	}
	r := httptest.NewRequest("GET", "/unknown/private-id", nil)
	r.Header.Set("traceparent", "malformed-private-id")
	handler.ServeHTTP(httptest.NewRecorder(), r)
	spans := exporter.GetSpans()
	if len(spans) != 1 || spans[0].Status.Code != codes.Error || spans[0].Name != "unmatched" || !spans[0].SpanContext.IsValid() || spans[0].Parent.IsValid() {
		t.Fatalf("invalid error/root trace: %v", spans)
	}
}
