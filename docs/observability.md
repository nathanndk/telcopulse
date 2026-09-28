# Operational measurements

All Go services expose `/metrics` on their internal HTTP listener (loopback in native development). Compose does not publish service ports; only Prometheus 9090 and Grafana 3002 are bound to host loopback. Aggregate metrics contain no raw URL query, transaction ID, customer ID or MSISDN labels. No metrics are exposed through the frontend's `/api` proxy.

## Signals

- `http_requests_total{service,method,route,status}` and `http_request_duration_seconds`: matched routes, bounded methods/statuses; health probes and scrapes excluded.
- `transaction_total{environment,status}` and `transaction_duration_seconds`: newly committed SUCCESS/FAILED business outcomes observed by the gateway. Idempotent replay does not increment these. Recovery completion does. HTTP 200/201 can carry a FAILED business outcome.
- `database_connection_usage` / `database_connections_max`: acquired and configured pgx pool connections per process.
- Go/runtime and process CPU/memory collectors: process measurements, not host or Kubernetes resource measurements.
- `event_outbox_pending`, `event_outbox_oldest_age_seconds`, `event_outbox_collection_success`: per-service durable publication backlog. Collection failures omit backlog readings.
- `kafka_consumer_lag{group,topic,partition}`: Kafka end offsets minus committed offsets for the notification consumer group. `kafka_lag_collection_success=0` signals missing/failed collection; missing lag is never represented as zero.

HTTP/business counters reset on process restart. Business counting is process-local telemetry, not an accounting ledger: a crash between database commit and observation may miss an increment. PostgreSQL transaction records remain authoritative. PromQL rates handle resets; idle success/error ratios are undefined, not fabricated 100% health.

## Monitoring configuration

Incident detail now retrieves a bounded historical range from Prometheus through incident-service. It computes five fixed 5-minute-rate series over a detection-aligned 2/24/72-hour window, with at most 120 requested evaluation points and no browser-supplied PromQL. Synthetic business success and transactions per minute use the incident environment; HTTP request rate, 5xx fraction and P95 use the affected service's shared process across development and staging. Empty, NaN and idle ratio samples are not presented as healthy values. The charts label their source and scope; the current five-minute service snapshot remains a separate panel. See the [Prometheus range-query API](https://prometheus.io/docs/prometheus/latest/querying/api/#range-queries).

Prometheus configuration and tested rule fixtures are under `infrastructure/prometheus`. Rules detect service scrape failure, paired-window synthetic purchase SLO burn (5m/1h at 14.4× or 30m/6h at 6×, with short-window traffic floors), high HTTP P95 with a twenty-request floor, notification lag, delayed event publication, unavailable lag collection and log-export loss. The incident service polls these rules and creates deduplicated TelcoPulse incidents for supported alert episodes; this is snapshot polling, not durable alert delivery. External paging is not configured. See the [incident ingestion contract](incidents.md) and [runbooks](runbooks/README.md).

The provisioned Grafana dashboard is `/d/telcopulse-operations` on port 3002. It uses measured Prometheus data for business success, throughput, HTTP latency/errors, lag, backlog, connection use, CPU and memory. Anonymous local access is Viewer-only; editing/login is disabled in this local demonstration. This is not production SSO/RBAC.

Monitoring configuration is copied into dedicated images because Docker on this workstation denied bind mounting the Documents directory. Persistent Prometheus and Grafana state uses named volumes. Prometheus local retention is capped at two days/256 MB (WAL/head overhead can exceed that cap).

## Validation commands

```sh
docker compose run --rm --no-deps --entrypoint promtool prometheus check config /etc/prometheus/prometheus.yml
docker compose run --rm --no-deps -w /etc/prometheus --entrypoint promtool prometheus test rules rules.test.yml
WEB_PORT=3001 python3 tests/integration/metrics.py
```

The live test intentionally creates synthetic business failures and pauses the notification consumer to prove alerting and lag recovery. It restores the consumer afterward. Current verification status is tracked in `VERIFICATION.md`.

The local stack now exports distributed OTel spans to Jaeger and can optionally export structured logs to Splunk HEC. Real Splunk indexing, Dynatrace ingestion, host/Kubernetes metrics and durable external alert delivery remain unfinished. Stored workflow stage references are not exported span IDs.

## Distributed tracing (HTTP and PostgreSQL)

Each Go process installs an OpenTelemetry SDK provider. Set `OTEL_EXPORTER_OTLP_ENDPOINT` to a reachable OTLP HTTP base URL (for example `http://collector:4318` on the Compose network) to export protobuf traces. Native runs may leave the endpoint empty for propagation only. Compose defaults to `http://jaeger:4318`; explicitly setting an empty value disables export. The default pipeline uses parent-based sampling with all new roots sampled, a bounded 2048-span queue, batches of 256, and bounded export/shutdown timeouts. Telemetry congestion may drop spans without blocking purchases.

HTTP server spans use matched routes, and internal RPC client spans inject W3C `traceparent`/`tracestate`. Completion logs contain the active trace/span IDs, route, status and duration. PostgreSQL query spans are children of the active request and record only an allowlisted operation name and generic failure status. Raw SQL/arguments, URLs, payloads and authorization headers are excluded. Untraced background queries do not create root spans.

New purchases retain the initial active trace ID as their durable correlation ID. Replay/recovery requests can have different active trace IDs; transaction step IDs are still workflow display identifiers, not claims of exported spans. Vendor ingestion remains subsequent work. Workflow recovery links are described below. Kafka propagation is now implemented as described below.

The HTTP export test decodes actual OTLP protobuf requests received over HTTP and verifies the gateway → client → downstream-server parent chain, W3C trace continuity and privacy exclusions. The PostgreSQL test uses `TEST_DATABASE_URL` to verify successful/failed child query spans against a real database. Implementation follows the [OpenTelemetry Go exporter guidance](https://opentelemetry.io/docs/languages/go/exporters/).


## Local trace viewer

The incident investigation page also queries Jaeger's stable v3 HTTP API through incident-service. Compose sets `JAEGER_QUERY_URL=http://jaeger:16686`; an empty value disables server-side lookup. The search is fixed to the incident's service and short detection window, then checks a returned span's `deployment.environment.name` before projecting a small service/database graph. It deliberately omits arbitrary span tags and does not infer root cause from a slow edge. Jaeger is a local verification backend; a real Dynatrace trace/topology lookup remains separate work. See [Jaeger v3 read API](https://www.jaegertracing.io/docs/2.21/architecture/apis/).

Compose includes Jaeger 2.21.0 with OTLP/HTTP ingestion on the internal network and its viewer at [localhost:16686](http://localhost:16686). The local backend retains at most 5,000 traces in memory, applies a 256 MiB memory limiter, and has a 384 MiB container limit. Traces disappear on restart; this is local investigation storage, not a production retention policy. No host OTLP port is published.

Run `WEB_PORT=3001 python3 tests/integration/traces.py` after starting the stack (use your frontend port). It creates a synthetic purchase, retrieves its durable trace ID, polls Jaeger's query API, and verifies five-service coverage, database spans, all three event-topic producer spans, the notification consumer span and parent references. It prints the actual trace viewer link. Partial exporter batches may appear briefly while spans arrive. Exporting does not gate transaction completion.

The configuration follows the official [Jaeger v2 deployment model](https://www.jaegertracing.io/docs/2.21/architecture/). Jaeger provides a local verification surface; Splunk and Dynatrace integration remain required by the project specification.

## Kafka trace continuity

Migration 007 adds a `traceparent` transport column to each service-owned outbox. The originating W3C identity commits atomically alongside the event without changing its immutable JSON payload. Every relay attempt starts a distinct producer span under that stored parent; broker headers carry the attempt's context to notification processing. A failed publish is marked as an error. Each processing attempt has a consumer span, and the notification-delivered event stores that consumer context. Invalid records retained in quarantine are marked as trace errors even when safely acknowledged.

Legacy outbox rows or records without valid metadata remain deliverable and start fresh traces. Only trace identity is transported: no baggage, credentials, SQL or subscriber attributes. A retried message can produce multiple attempt spans while database deduplication still yields one logical receipt. Workflow recovery links are covered by the following increment.

## Workflow recovery links

Migration 008 preserves the original request's traceparent in each durable workflow. Every resumed execution emits a `purchase.resume` span with transaction ID, environment and outcome. Request-driven attempts retain their current HTTP parent; background recovery starts a new trace. Both link to the original request span, rather than extending an ended request's lifetime. Failed execution attempts are marked as errors, and retry logs distinguish the active trace ID from the original correlation trace ID. Legacy workflows without an originating context remain recoverable without a fabricated link.

`WEB_PORT=3001 python3 tests/integration/trace_recovery.py` stops the local payment service, creates a pending synthetic E-Wallet purchase, restores payment in a finally block, waits for autonomous recovery and queries Jaeger's v3 trace summaries plus trace details to verify the `FOLLOWS_FROM` reference to the original request.

## Operator trace navigation

Set the frontend's server-side `TRACE_VIEWER_URL` to the public Jaeger base URL. Compose defaults to `http://localhost:16686`; an explicit empty value disables links. The detail page reads this at request time, so no frontend rebuild is required for a URL change. Only HTTP(S) URLs without credentials, query strings or fragments are accepted.

“Open original trace” uses the transaction's original trace ID. “Find workflow attempts” searches the last two days for `purchase.resume` spans tagged with that transaction ID, including linked background recovery traces. The viewer opens in a new tab. Export is asynchronous and local retention is ephemeral; the page explains why a recent or old trace may not be available. Persisted workflow step IDs are labeled “Stage reference” rather than “Span ID.”

The trace-navigation browser tests require a configured, running Jaeger viewer and the full application stack. When testing a separate local frontend against the Compose API, set `PURCHASE_URL` to the Compose frontend origin for test setup and `WEB_URL` to the frontend under test.
