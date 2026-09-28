package telemetry_test

import (
	"context"
	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"os"
	"strings"
	"telcopulse/services/shared/telemetry"
	"testing"
)

func TestPostgresSpansExcludeSQLAndArguments(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL required for real PostgreSQL tracing")
	}
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
	config, err := pgx.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	config.Tracer = telemetry.DatabaseTracer{}
	conn, err := pgx.ConnectConfig(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := conn.Close(context.Background()); err != nil {
			t.Error(err)
		}
	}()
	// Polls without a request context should not generate disconnected root spans.
	if _, err := conn.Exec(context.Background(), "SELECT 1"); err != nil {
		t.Fatal(err)
	}
	if len(exporter.GetSpans()) != 0 {
		t.Fatal("background query created a root trace")
	}
	ctx, parent := provider.Tracer("test").Start(context.Background(), "purchase")
	var result string
	if err := conn.QueryRow(ctx, "SELECT $1::text", "subscriber-secret").Scan(&result); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, "SELECT 1/0 /* subscriber-secret */"); err == nil {
		t.Fatal("expected database failure")
	}
	parent.End()
	spans := exporter.GetSpans()
	if len(spans) != 3 {
		t.Fatalf("expected query, failed query, parent; got %d", len(spans))
	}
	for i, span := range spans[:2] {
		if span.Name != "postgresql SELECT" || span.Parent.SpanID() != parent.SpanContext().SpanID() {
			t.Fatalf("wrong database span: %v", span)
		}
		if strings.Contains(span.Status.Description, "subscriber-secret") {
			t.Fatal("error leaked query data")
		}
		for _, attr := range span.Attributes {
			if strings.Contains(attr.Value.AsString(), "subscriber-secret") {
				t.Fatal("attribute leaked query data")
			}
		}
		if i == 1 && span.Status.Code != codes.Error {
			t.Fatal("database failure not marked")
		}
	}
}
