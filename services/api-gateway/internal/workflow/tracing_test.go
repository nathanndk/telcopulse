package workflow

import (
	"context"
	"errors"
	"github.com/twmb/franz-go/pkg/kgo"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	notification "telcopulse/services/notification-service"
	"telcopulse/services/shared/events"
	"testing"
)

func TestDurableEventTraceAcrossRetries(t *testing.T) {
	s := setup(t, nil, nil, nil)
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
	ctx, root := provider.Tracer("test").Start(context.Background(), "purchase")
	_, _, err := s.Purchase(ctx, purchaseInput(), "trace-retry-purchase")
	if err != nil {
		t.Fatal(err)
	}
	root.End()
	var parent string
	if err := s.Pool.QueryRow(context.Background(), "SELECT traceparent FROM event_outbox").Scan(&parent); err != nil {
		t.Fatal(err)
	}
	var origin trace.SpanContext
	for _, span := range exporter.GetSpans() {
		if span.Name == "purchase.resume" {
			origin = span.SpanContext
		}
	}
	if parent != events.TraceParent(trace.ContextWithSpanContext(context.Background(), origin)) {
		t.Fatal("outbox did not persist originating context")
	}
	// Detached worker context represents publication after the originating request ended.
	failed := errors.New("broker unavailable")
	_, err = events.PublishOne(context.Background(), s.Pool, func(context.Context, string, string, []byte) error { return failed })
	if !errors.Is(err, failed) {
		t.Fatal("publication failure not retained")
	}
	if _, err = s.Pool.Exec(context.Background(), "UPDATE event_outbox SET available_at=now()"); err != nil {
		t.Fatal(err)
	}
	var record *kgo.Record
	var produced trace.SpanContext
	_, err = events.PublishOne(context.Background(), s.Pool, func(ctx context.Context, topic, key string, payload []byte) error {
		produced = trace.SpanContextFromContext(ctx)
		record = &kgo.Record{Topic: topic, Key: []byte(key), Value: payload, Headers: []kgo.RecordHeader{{Key: "traceparent", Value: []byte(events.TraceParent(ctx))}}}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err = notification.Process(context.Background(), s.Pool, record); err != nil {
			t.Fatal(err)
		}
	}
	if count(t, s, "notification.deliveries") != 1 {
		t.Fatal("trace metadata changed deduplication")
	}
	var deliveryParent string
	if err = s.Pool.QueryRow(context.Background(), "SELECT traceparent FROM notification.event_outbox").Scan(&deliveryParent); err != nil {
		t.Fatal(err)
	}
	if deliveryParent == "" {
		t.Fatal("delivery event lost consumer trace")
	}
	producers, consumers, failures := 0, 0, 0
	for _, span := range exporter.GetSpans() {
		switch span.SpanKind {
		case trace.SpanKindProducer:
			producers++
			if span.Parent.SpanID() != origin.SpanID() {
				t.Fatal("producer lost durable parent")
			}
			if span.Status.Code == codes.Error {
				failures++
			}
		case trace.SpanKindConsumer:
			consumers++
			if span.Parent.SpanID() != produced.SpanID() || span.SpanContext.TraceID() != root.SpanContext().TraceID() {
				t.Fatal("consumer lost producer parent")
			}
		}
	}
	if producers != 2 || consumers != 2 || failures != 1 {
		t.Fatalf("attempt spans: producers=%d consumers=%d failures=%d", producers, consumers, failures)
	}
}

func TestBackgroundRecoveryLinksOriginalRequest(t *testing.T) {
	var once atomic.Bool
	s := setup(t, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/internal/reserve" && once.CompareAndSwap(false, true) {
				next.ServeHTTP(httptest.NewRecorder(), r)
				http.Error(w, "lost response", http.StatusServiceUnavailable)
				return
			}
			next.ServeHTTP(w, r)
		})
	}, nil, nil)
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
	ctx, root := provider.Tracer("test").Start(context.Background(), "original request")
	result, _, err := s.Purchase(ctx, purchaseInput(), "linked-background-recovery")
	root.End()
	if !errors.Is(err, ErrPending) {
		t.Fatalf("want pending: %v", err)
	}
	if _, err = s.Pool.Exec(context.Background(), "UPDATE purchase_workflows SET updated_at=now()-interval '10 seconds'"); err != nil {
		t.Fatal(err)
	}
	s.recoverBatch(context.Background())
	recovered, err := s.Transaction(context.Background(), result.ID)
	if err != nil || recovered.Status != "SUCCESS" {
		t.Fatalf("recovery failed: %v", err)
	}
	if balance(t, s) != 200000 || count(t, s, "payment.reservations") != 1 {
		t.Fatal("recovery duplicated debit")
	}
	attempts := 0
	independent := false
	for _, span := range exporter.GetSpans() {
		if span.Name != "purchase.resume" {
			continue
		}
		attempts++
		if len(span.Links) != 1 || span.Links[0].SpanContext.SpanID() != root.SpanContext().SpanID() {
			t.Fatal("attempt missing original request link")
		}
		if !span.Parent.IsValid() {
			independent = true
			if span.SpanContext.TraceID() == root.SpanContext().TraceID() {
				t.Fatal("background recovery reused original trace")
			}
		}
	}
	if attempts != 2 || !independent {
		t.Fatalf("attempts=%d independent=%t", attempts, independent)
	}
}
