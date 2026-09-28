# Dynatrace trace export

TelcoPulse traces use vendor-neutral OpenTelemetry spans. The existing local Jaeger endpoint remains the default. The optional overlay directs the eight Go services to a configured Dynatrace OTLP/HTTP base endpoint and reads an Authorization header value from a mounted secret file.

The base URL should be HTTPS and end in `/api/v2/otlp`; the exporter appends `/v1/traces`. A complete trace URL ending in `/v1/traces` is also accepted. The code rejects credentials embedded in the URL, query strings, fragments and HTTP when a secret-file authorization value is present. It refuses redirects, preserves system certificate verification, and accepts an extra CA bundle through `OTEL_EXPORTER_OTLP_CERTIFICATE`. The secret file is limited to 4 KiB; its path and contents are omitted from error text. Avoid also setting OTLP header environment variables when using the secret file.

For a Classic Dynatrace token with `openTelemetryTrace.ingest`, the file should contain one line in the form `Api-Token <token>`. A platform token uses `Bearer <token>` and the relevant trace ingest scope. Store it outside the repository and configure `DYNATRACE_AUTHORIZATION_PATH` with the private file path. Configure `DYNATRACE_OTLP_ENDPOINT` with your own Dynatrace base URL, then use:

```sh
docker compose -f compose.yaml -f infrastructure/dynatrace/compose.yaml config --quiet
docker compose -f compose.yaml -f infrastructure/dynatrace/compose.yaml build api-gateway subscriber-service package-service payment-service notification-service incident-service simulation-service deployment-service
docker compose -f compose.yaml -f infrastructure/dynatrace/compose.yaml up -d
```

The overlay adds an outbound trace network and mounts the authorization file. It has not been enabled against a real Dynatrace environment. A trusted local TLS receiver test verifies the authorization header, `/api/v2/otlp/v1/traces` path and protobuf payload. Real verification must find a generated transaction's trace in Dynatrace with the matching trace ID and service spans. Authenticated OTLP acceptance alone would not prove that the trace is queryable or connected to infrastructure topology.

Protocol reference: [Dynatrace OTLP endpoints and token scopes](https://docs.dynatrace.com/docs/ingest-from/opentelemetry/otlp-api).
