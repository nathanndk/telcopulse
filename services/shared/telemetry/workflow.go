package telemetry

import (
	"context"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// WorkflowAttempt preserves the current request parent, if any, and links to the
// original durable request. Background recovery starts its own trace rather than
// inventing an active parent for a request that has already ended.
func WorkflowAttempt(ctx context.Context, origin, transaction, environment string) (context.Context, trace.Span) {
	options := []trace.SpanStartOption{trace.WithAttributes(attribute.String("transaction.id", transaction), attribute.String("deployment.environment.name", environment))}
	if len(origin) == 55 {
		extracted := propagation.TraceContext{}.Extract(context.Background(), propagation.MapCarrier{"traceparent": origin})
		original := trace.SpanContextFromContext(extracted)
		if original.IsValid() {
			options = append(options, trace.WithLinks(trace.Link{SpanContext: original}))
		}
	}
	return otel.Tracer("telcopulse/workflow").Start(ctx, "purchase.resume", options...)
}
