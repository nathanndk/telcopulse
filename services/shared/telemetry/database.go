package telemetry

import (
	"context"
	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"strings"
)

// DatabaseTracer records query duration and outcome without SQL, arguments,
// connection strings, or server error text, which may contain subscriber data.
type DatabaseTracer struct{}

type querySpanKey struct{}

// TraceQueryStart starts a client span only for work with a valid parent trace.
// Background polling and readiness checks do not create endless root traces.
func (DatabaseTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if !trace.SpanContextFromContext(ctx).IsValid() {
		return ctx
	}
	operation := "QUERY"
	fields := strings.Fields(data.SQL)
	if len(fields) > 0 {
		candidate := strings.ToUpper(fields[0])
		switch candidate {
		case "SELECT", "INSERT", "UPDATE", "DELETE", "BEGIN", "COMMIT", "ROLLBACK":
			operation = candidate
		}
	}
	ctx, span := otel.Tracer("telcopulse/postgres").Start(ctx, "postgresql "+operation, trace.WithSpanKind(trace.SpanKindClient), trace.WithAttributes(attribute.String("db.system.name", "postgresql"), attribute.String("db.operation.name", operation)))
	return context.WithValue(ctx, querySpanKey{}, span)
}

// TraceQueryEnd closes this query's span and records only a generic error status.
func (DatabaseTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	span, ok := ctx.Value(querySpanKey{}).(trace.Span)
	if !ok {
		return
	}
	if data.Err != nil {
		span.SetStatus(codes.Error, "database query failed")
	}
	span.End()
}
