package telemetry

import (
	"context"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/protobuf/proto"
)

func TestAuthenticatedTraceExportPreservesBasePath(t *testing.T) {
	received := make(chan error, 1)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/otlp/v1/traces" || r.Header.Get("Authorization") != "Api-Token integration-only" {
			received <- errors.New("wrong path or authorization")
			return
		}
		data, err := io.ReadAll(r.Body)
		if err != nil {
			received <- err
			return
		}
		var payload collector.ExportTraceServiceRequest
		if err = proto.Unmarshal(data, &payload); err != nil || len(payload.ResourceSpans) == 0 {
			received <- errors.New("invalid OTLP protobuf")
			return
		}
		received <- nil
		w.Header().Set("Content-Type", "application/x-protobuf")
		_, _ = w.Write([]byte{})
	}))
	defer server.Close()
	cert := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	certPath := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(certPath, cert, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OTEL_EXPORTER_OTLP_CERTIFICATE", certPath)
	provider, err := tracing(context.Background(), "payment-service", server.URL+"/api/v2/otlp", "Api-Token integration-only")
	if err != nil {
		t.Fatal(err)
	}
	_, span := provider.Tracer("test").Start(context.Background(), "purchase")
	span.End()
	if err = provider.ForceFlush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err = <-received; err != nil {
		t.Fatal(err)
	}
	if err = provider.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestTraceAuthorizationConfiguration(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_AUTHORIZATION_FILE", "")
	if value, err := traceAuthorization(); err != nil || value != "" {
		t.Fatal("unconfigured authorization")
	}
	path := filepath.Join(t.TempDir(), "authorization")
	if err := os.WriteFile(path, []byte("Api-Token secret\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OTEL_EXPORTER_OTLP_AUTHORIZATION_FILE", path)
	if value, err := traceAuthorization(); err != nil || value != "Api-Token secret" {
		t.Fatal("token file parsing", err)
	}
	t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "Authorization=other")
	if _, err := traceAuthorization(); err == nil {
		t.Fatal("conflicting headers")
	}
	t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "")
	if err := os.WriteFile(path, []byte(strings.Repeat("x", 4097)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := traceAuthorization(); err == nil {
		t.Fatal("oversized authorization")
	}
	if _, err := tracing(context.Background(), "test", "http://localhost:4318", "Api-Token secret"); err == nil {
		t.Fatal("plaintext authenticated export")
	}
	for _, endpoint := range []string{"https://user:password@example.com/api/v2/otlp", "https://example.com/api/v2/otlp?token=x", "file:///api/v2/otlp"} {
		if provider, err := tracing(context.Background(), "test", endpoint, ""); err == nil {
			_ = provider.Shutdown(context.Background())
			t.Fatalf("accepted %q", endpoint)
		}
	}
}

func TestAuthenticatedTraceClientRefusesRedirect(t *testing.T) {
	reached := false
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true }))
	defer target.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer origin.Close()
	client, err := authenticatedTraceClient()
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Get(origin.URL)
	if response != nil {
		_ = response.Body.Close()
	}
	if err == nil || reached {
		t.Fatal("authenticated client followed redirect")
	}
}
