package telemetry

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// Tracing creates a bounded asynchronous OTLP/HTTP pipeline. An empty endpoint
// enables local context propagation without claiming remote trace storage.
func Tracing(ctx context.Context, service, endpoint string) (*sdktrace.TracerProvider, error) {
	return tracing(ctx, service, endpoint, "")
}

func tracing(ctx context.Context, service, endpoint, authorization string) (*sdktrace.TracerProvider, error) {
	options := []sdktrace.TracerProviderOption{
		sdktrace.WithResource(resource.NewSchemaless(attribute.String("service.name", service), attribute.String("service.version", "0.2.0"))),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.AlwaysSample())),
	}
	if endpoint != "" {
		u, err := url.Parse(endpoint)
		if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") {
			return nil, errors.New("invalid OTLP endpoint")
		}
		if authorization != "" && u.Scheme != "https" {
			return nil, errors.New("authenticated OTLP requires HTTPS")
		}
		if !strings.HasSuffix(strings.TrimRight(u.Path, "/"), "/v1/traces") {
			u.Path = strings.TrimRight(u.Path, "/") + "/v1/traces"
		}
		exporterOptions := []otlptracehttp.Option{otlptracehttp.WithEndpointURL(u.String()), otlptracehttp.WithTimeout(2 * time.Second)}
		if authorization != "" {
			client, err := authenticatedTraceClient()
			if err != nil {
				return nil, err
			}
			exporterOptions = append(exporterOptions, otlptracehttp.WithHeaders(map[string]string{"Authorization": authorization}), otlptracehttp.WithHTTPClient(client))
		}
		exporter, err := otlptracehttp.New(ctx, exporterOptions...)
		if err != nil {
			return nil, err
		}
		options = append(options, sdktrace.WithBatcher(exporter, sdktrace.WithMaxQueueSize(2048), sdktrace.WithMaxExportBatchSize(256), sdktrace.WithBatchTimeout(time.Second), sdktrace.WithExportTimeout(3*time.Second)))
	}
	return sdktrace.NewTracerProvider(options...), nil
}

// InstallTracing configures this service's provider; callers must stop workers
// and HTTP handlers before invoking the bounded shutdown function.
func InstallTracing(ctx context.Context, service string, logger *slog.Logger) (func(), error) {
	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	authorization, err := traceAuthorization()
	if err != nil {
		return nil, err
	}
	if authorization != "" && endpoint == "" {
		return nil, errors.New("OTLP authorization requires an endpoint")
	}
	provider, err := tracing(ctx, service, endpoint, authorization)
	if err != nil {
		return nil, err
	}
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	logger.Info("tracing configured", "export_enabled", endpoint != "")
	return func() {
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := provider.Shutdown(shutdown); err != nil {
			logger.Error("trace shutdown failed", "error", err)
		}
	}, nil
}

// TraceID returns a valid active trace ID, or an empty string when uninstrumented.
func TraceID(ctx context.Context) string {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return ""
	}
	return sc.TraceID().String()
}

// TraceHTTP records server spans and correlated completion logs without raw URLs,
// bodies, credentials, subscriber identifiers, or arbitrary incoming headers.
func TraceHTTP(next http.Handler, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/metrics" || r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
			next.ServeHTTP(w, r)
			return
		}
		parent := propagation.TraceContext{}.Extract(r.Context(), propagation.HeaderCarrier(r.Header))
		ctx, span := otel.Tracer("telcopulse/http").Start(parent, "HTTP", trace.WithSpanKind(trace.SpanKindServer))
		defer span.End()
		started := time.Now()
		bounded := r.WithContext(ctx)
		writer := &response{ResponseWriter: w}
		next.ServeHTTP(writer, bounded)
		r.Pattern = bounded.Pattern
		route := r.Pattern
		if route == "" {
			route = "unmatched"
		}
		status := writer.status
		if status == 0 {
			status = 200
		}
		span.SetName(route)
		span.SetAttributes(attribute.String("http.route", route), attribute.Int("http.response.status_code", status))
		if status >= 500 {
			span.SetStatus(codes.Error, "server error")
		}
		sc := span.SpanContext()
		logger.InfoContext(ctx, "HTTP request completed", "route", route, "status", status, "duration_ms", float64(time.Since(started).Microseconds())/1000, "trace_id", sc.TraceID().String(), "span_id", sc.SpanID().String())
	})
}

// traceAuthorization loads a secret without exposing its path or contents in errors.
func traceAuthorization() (string, error) {
	path := os.Getenv("OTEL_EXPORTER_OTLP_AUTHORIZATION_FILE")
	if path == "" {
		return "", nil
	}
	if os.Getenv("OTEL_EXPORTER_OTLP_HEADERS") != "" || os.Getenv("OTEL_EXPORTER_OTLP_TRACES_HEADERS") != "" {
		return "", errors.New("configure only one OTLP authorization source")
	}
	file, err := os.Open(path)
	if err != nil {
		return "", errors.New("cannot read OTLP authorization file")
	}
	data, readErr := io.ReadAll(io.LimitReader(file, 4097))
	closeErr := file.Close()
	value := strings.TrimSpace(string(data))
	if readErr != nil || closeErr != nil || len(data) > 4096 || value == "" || strings.ContainsAny(value, "\r\n") {
		return "", errors.New("invalid OTLP authorization file")
	}
	return value, nil
}

func authenticatedTraceClient() (*http.Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if path := os.Getenv("OTEL_EXPORTER_OTLP_CERTIFICATE"); path != "" {
		file, err := os.Open(path)
		if err != nil {
			return nil, errors.New("cannot read OTLP CA file")
		}
		data, readErr := io.ReadAll(io.LimitReader(file, 1<<20+1))
		closeErr := file.Close()
		if readErr != nil || closeErr != nil || len(data) > 1<<20 {
			return nil, errors.New("invalid OTLP CA file")
		}
		roots, err := x509.SystemCertPool()
		if err != nil {
			return nil, errors.New("cannot load system CA certificates")
		}
		if !roots.AppendCertsFromPEM(data) {
			return nil, errors.New("invalid OTLP CA certificates")
		}
		transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	}
	return &http.Client{Transport: transport, Timeout: 2 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("OTLP redirect refused") }}, nil
}
