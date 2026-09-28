// Package rpc supplies authenticated, deadline-bounded domain HTTP calls.
package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"io"
	"log/slog"
	"net/http"
	"telcopulse/services/shared/runtime"
	"time"
)

// Error preserves a remote response status without exposing response internals.
type Error struct {
	Status  int
	Service string
}

func (e *Error) Error() string { return fmt.Sprintf("%s returned HTTP %d", e.Service, e.Status) }

// Client calls a configured internal service.
type Client struct {
	URL           string
	Token         string
	MutationToken string
	HTTP          *http.Client
}

// New creates a client with no automatic mutation retries; workflows own retry policy.
func New(url, token string) *Client {
	return &Client{URL: url, Token: token, HTTP: &http.Client{Timeout: 4 * time.Second}}
}

// Response preserves the status and bounded JSON body of an internal resource request.
type Response struct {
	Status int
	Body   json.RawMessage
}

// Call propagates correlation identity and decodes a typed successful response.
func (c *Client) Call(ctx context.Context, method, path string, input, output any, traceID, transactionID string) error {
	result, err := c.exchange(ctx, method, path, input, "", traceID, transactionID, "", "")
	if err != nil {
		return err
	}
	if result.Status < 200 || result.Status >= 300 {
		return &Error{Status: result.Status, Service: c.URL}
	}
	if err = json.Unmarshal(result.Body, output); err != nil {
		return fmt.Errorf("decode domain result: %w", err)
	}
	return nil
}

// Exchange forwards a resource request without automatic retries. Only the explicit
// creation key crosses the boundary; browser cookies and authorization never do.
func (c *Client) Exchange(ctx context.Context, method, path string, input any, key string) (Response, error) {
	return c.exchange(ctx, method, path, input, key, "", "", "", "")
}

// ExchangeAs adds an identity and role derived from a validated operator session.
func (c *Client) ExchangeAs(ctx context.Context, method, path string, input any, key, actor, role string) (Response, error) {
	return c.exchange(ctx, method, path, input, key, "", "", actor, role)
}
func (c *Client) exchange(ctx context.Context, method, path string, input any, key, traceID, transactionID, actor, role string) (out Response, callErr error) {
	ctx, span := otel.Tracer("telcopulse/rpc").Start(ctx, "internal HTTP", trace.WithSpanKind(trace.SpanKindClient))
	defer func() {
		if callErr != nil || out.Status >= 400 {
			span.SetStatus(codes.Error, "domain call failed")
		}
		span.End()
	}()
	var payload []byte
	var err error
	if input != nil {
		payload, err = json.Marshal(input)
		if err != nil {
			return out, err
		}
	}
	request, err := http.NewRequestWithContext(ctx, method, c.URL+path, bytes.NewReader(payload))
	if err != nil {
		return out, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+c.Token)
	if c.MutationToken != "" && method != http.MethodGet && method != http.MethodHead {
		request.Header.Set(runtime.MutationTokenHeader, c.MutationToken)
	}
	request.Header.Set("X-Trace-ID", traceID)
	request.Header.Set("X-Transaction-ID", transactionID)
	if key != "" {
		request.Header.Set("Idempotency-Key", key)
	}
	if actor != "" {
		request.Header.Set("X-Operator-Actor", actor)
		request.Header.Set("X-Operator-Role", role)
	}
	propagation.TraceContext{}.Inject(ctx, propagation.HeaderCarrier(request.Header))
	response, err := c.HTTP.Do(request)
	if err != nil {
		return out, fmt.Errorf("domain call: %w", err)
	}
	defer func() {
		if err := response.Body.Close(); err != nil {
			slog.Warn("close domain response", "error", err)
		}
	}()
	out.Status = response.StatusCode
	span.SetAttributes(attribute.Int("http.response.status_code", out.Status))
	// Resource pages are bounded on both sides; refuse oversized or malformed output.
	const maxResponse = 8 << 20
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponse+1))
	if err != nil {
		return out, fmt.Errorf("read domain result: %w", err)
	}
	if len(body) > maxResponse || !json.Valid(body) {
		return out, fmt.Errorf("invalid or oversized domain JSON response")
	}
	out.Body = body
	return out, nil
}
