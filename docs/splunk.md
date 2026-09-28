# Splunk integration

The shared `logexport` package implements a Splunk HTTP Event Collector (HEC) event transport. It wraps existing structured JSON as `event`, specifies `_json` sourcetype and optionally selects an index. Correlation fields remain inside the event. Application instrumentation stays independent of this transport.

The endpoint must end in `/services/collector/event`, use HTTPS and contain no embedded credentials, query or fragment. Plain HTTP is allowed only for explicitly enabled loopback development receivers. TLS verification is never disabled. Authentication uses an Authorization header; returned errors exclude server bodies, tokens and endpoints. Requests have a three-second deadline, events a 64 KiB limit and responses a 4 KiB limit. Redirects are refused. Acceptance requires HTTP 200 and HEC code zero.

Optional application wiring, bounded asynchronous buffering, delivery metrics and shutdown behavior are implemented below. An opt-in Compose overlay and configured-logger tests against a local receiver are available. Production images include the optional exporter with export disabled. Real Splunk indexing verification remains pending. No real Splunk endpoint or credential is configured. Local receiver tests verify the protocol, not actual indexing. HEC acceptance does not prove durable indexing; indexer acknowledgment and durable retry requirements must be evaluated before production use.

[Example investigation queries](../infrastructure/splunk/queries.spl) exclude replay logs and deduplicate transaction identity before computing business outcomes. Set the Splunk time picker to the incident period. These queries are authored against current log fields but have not been executed against a real Splunk index.

Protocol references: [Splunk event format](https://help.splunk.com/en/splunk-enterprise/get-data-in/get-started-with-getting-data-in/10.0/get-data-with-http-event-collector/format-events-for-http-event-collector) and [HEC response codes](https://help.splunk.com/en/splunk-cloud-platform/get-data-in/get-started-with-getting-data-in/9.2.2406/get-data-with-http-event-collector/troubleshoot-http-event-collector).

## Optional application export

Gateway and shared service startup now read `SPLUNK_HEC_URL`, `SPLUNK_HEC_TOKEN` and optional `SPLUNK_HEC_INDEX`. URL and token must be configured together. No export occurs when both are absent. Configure credentials through deployment secrets, never browser settings or source files. The optional Compose overlay mounts a token file; current running images include this integration with export disabled.

Export copies each structured log record into a 256-record queue, capped at 64 KiB per record. Stdout remains enabled. Application writes do not wait on the receiver; full queues, oversized records and writes after shutdown increment dropped counts. One worker sends records with the transport deadline. Failed attempts are counted without retry. Shutdown drains for up to five seconds, then cancels pending delivery. This is best-effort process memory, not a durable log delivery guarantee.

When enabled, `/metrics` exposes `log_export_events_total{outcome="accepted|failed|dropped"}` and `log_export_pending`. Acceptance means HEC code zero, not confirmed indexing. Pending excludes the currently in-flight request. Counters reset on process restart.

## Compose configuration

Create a private token file outside the repository, then set `SPLUNK_HEC_TOKEN_PATH` to its absolute path. Configure `SPLUNK_HEC_URL` with the full HTTPS `/services/collector/event` URL and `SPLUNK_HEC_INDEX` with the allowed target index. Do not place the token itself in shell commands. The token file is read at startup, capped at 4 KiB and trimmed; supplying both a token and token file is rejected. Token rotation requires restarting the services.

From the repository root:

```sh
docker compose -f compose.yaml -f infrastructure/splunk/compose.yaml config --quiet
docker compose -f compose.yaml -f infrastructure/splunk/compose.yaml build api-gateway subscriber-service package-service payment-service notification-service incident-service simulation-service deployment-service
docker compose -f compose.yaml -f infrastructure/splunk/compose.yaml up -d
```

The overlay attaches Go services to an additional network permitting outbound log transport. It does not expose additional host ports. It mounts the same HEC credential into each configured service; use a restricted token limited to the intended index. Default Compose remains export-disabled. Real verification must show a generated transaction in the target Splunk index with matching trace/transaction IDs, then execute the example SPL queries; HEC code zero and local test receivers alone do not satisfy that check.

## Optional incident log search

The incident service can query indexed structured logs through Splunk's read-only Search API. Configure `SPLUNK_SEARCH_URL` as the full verified-HTTPS `/services/search/v2/jobs` endpoint, `SPLUNK_SEARCH_TOKEN_PATH` as a private token file outside the repository and `SPLUNK_SEARCH_INDEX` as the fixed index. Use the [search Compose overlay](../infrastructure/splunk/search.compose.yaml) alongside the base file. The Search API token is distinct from the HEC ingestion token and should have search access only to the intended index. Default Compose does not configure it.

Incident detail offers 2-, 24- and 72-hour windows after detection, including fifteen minutes before the detected time. The backend runs a fixed service/environment/index query, takes at most 50 events, caps responses at 256 KiB and sets an eight-second deadline. It selects only time, level, service, message, trace ID, transaction ID and error code, rechecks the returned scope, masks Indonesian mobile-number patterns in messages and never sends browser-supplied SPL. Invalid configuration fails startup; unavailable search returns a source error, while unconfigured search and zero results have distinct UI states. The local tests use a protocol receiver and do not prove that a real Splunk index has ingested data.

The API uses Splunk's [`search/jobs` one-shot mode](https://help.splunk.com/en/splunk-enterprise/rest-api-reference/10.2/search-endpoints/search-endpoint-descriptions) with JSON output, fixed time bounds and a bearer authentication token. A real integration check must generate a failure, confirm the event in the configured index and verify its trace and transaction correlation in incident detail.

## Detect missing exported logs

`LogExportLoss` fires when failed or dropped export counters increase in the five-minute window and the condition lasts thirty seconds. It routes a SEV-3 observability incident for the affected service, with explicit shared-runtime scope. Disabled exporters emit no export counters and do not trigger this rule. It does not prove receiver indexing health: accepted-but-unindexed data requires vendor-side monitoring. Counter resets and events lost before the first scrape may escape detection; process metrics are not a durable delivery ledger. Inspect stdout and the receiver before concluding that application logs are absent.
