# Public API

Base path: `/api/v1`. JSON responses; all data is synthetic. This phase only runs with `APP_MODE=local`.

| Method | Path | Behavior |
|---|---|---|
| GET | `/healthz` | Process liveness (outside API prefix) |
| GET | `/readyz` | Database readiness (outside API prefix) |
| GET | `/customers` | Masked subscriber catalog and balance |
| GET | `/packages` | Package catalog with integer IDR prices |
| POST | `/transactions` | Resumable distributed synthetic purchase |
| GET | `/transactions` | Paginated transaction history |
| GET | `/transactions/{id}` | Persisted result and stage evidence |
| GET | `/search` | Environment-scoped workspace navigation across incidents, transactions, deployments and simulations |
| GET | `/audit` | Paged operations audit across incident, simulation and deployment events |
| GET | `/overview` | Last-hour business metrics/minute buckets plus a rolling 30-day synthetic purchase SLO and error budget; use `environment=development|staging` |
| GET | `/incidents/operations` | Thirty-day incident count, SEV-1 count, MTTA/MTTR, escalation events and repeat candidates by environment |
| GET | `/incidents/actions` | Paged corrective actions from incident records, scoped to development or staging |
| GET | `/incidents/postmortems` | Paged immutable structured incident reports, scoped to development or staging |
| GET | `/incidents` | Paged incident summaries with state, severity, service, owner, title/ID and detection-time filters |
| GET | `/incidents/{id}/logs` | Bounded indexed log evidence for the incident's service and detection window, when Splunk Search is configured |
| GET | `/incidents/{id}/traces` | Bounded Jaeger trace/dependency evidence for the incident's service, environment and detection window |
| GET | `/incidents/{id}/metrics` | Timestamped incident-period Prometheus business and service HTTP measurements; only `window=2h|24h|72h` is forwarded |
| GET | `/incidents/{id}/infrastructure` | Optional Datadog pod restart, memory and CPU evidence scoped to the incident environment/service; only `window=2h|24h|72h` is forwarded |
| GET | `/incidents/{id}/purchase-impact` | Observed completed synthetic purchase cohort and paged failures in the persisted incident environment; accepts bounded `window` and `cursor` |
| GET | `/deployments/operations` | Thirty-day evaluated deployments, failures and failure rate by environment |
| GET | `/deployments` | Bounded CI-reported deployment events for a service and time window |
| GET | `/deployments/{id}` | Current deployment record and immutable status history |

Deployment event intake is internal to the service network, not a public gateway mutation. See [deployment tracking](deployments.md) for the authenticated event contract and its local-only boundary.

Workspace search accepts `environment=development|staging` and `q` of 2–80 characters. It returns at most five hits per source and 20 total, ranking exact and prefix IDs ahead of text matches. It searches transaction IDs and trace IDs; incident IDs, titles and services; deployment IDs, services and versions; and simulation IDs and scenarios. Results contain only source, resource ID, title, operational detail and timestamp. They exclude transaction result JSON, incident impact/snapshots and simulation reasons. Search is for quick navigation, not an exhaustive paginated report; each source page remains the place for broader filtering. The current literal substring search has not been benchmarked at production scale.

Corrective actions accept `environment=development|staging` (required), `status=active|all|Open|In Progress|Blocked|Completed` (default active), `priority=P1|P2|P3`, literal case-insensitive `owner` and `search` filters (up to 100 characters each), `limit` (default 25, maximum 100) and an opaque, filter-scoped `cursor`. The response is `{items, more, next}`. Rows include the source incident ID, title, state, severity and service, plus action position, title, owner, normalized priority/status and nullable due time. Unfinished actions sort before completed ones, then by earliest due time and priority. The API is read-only; changes use the incident's version-checked editor. Concurrent incident edits may shift offset-based pages, so refresh the first page before acting on older results.

Postmortems require `environment=development|staging` and accept exact `service`, exact `severity=SEV-1|SEV-2|SEV-3|SEV-4`, literal case-insensitive `search` across incident title/ID and frozen report narrative (up to 100 characters), `limit` (default 20, maximum 100), and an opaque filter-scoped `cursor`. The response is `{items, more, next}` ordered by report generation time and incident ID. Narrative excerpts come from the published snapshot; title, service and severity are current incident metadata. Older reports may have empty mitigation.

The incident list requires `environment=development|staging` and accepts exact `state`, `severity` and `service`, case-insensitive literal `owner` and `search` (incident title or ID), and RFC 3339 `since` for detection time. Service, owner and search are limited to 100 characters. Limit defaults to 50 and is capped at 100. Results sort by latest update, then incident ID, with an opaque cursor scoped to all filters. The console requests 20 per page and offers 24-hour, 7-day and 30-day detection presets.

List parameters: `environment=development|staging` (default development), `page` (default 1), `page_size` (default 20, max 100), `status=SUCCESS|FAILED|PROCESSING`, `search` (ID, trace ID or customer name; max 100 characters). Overview accepts environment. Unknown environments return 400.

Purchase requires `Content-Type: application/json` and `Idempotency-Key` (16–100 alphanumeric, underscore or hyphen characters).

```json
{
  "customer_id": "cus-001",
  "package_id": "pkg-10",
  "payment_method": "E-Wallet",
  "environment": "development"
}
```

Payment methods: Pulsa, E-Wallet, Credit Card, Virtual Account. Unknown fields, trailing JSON and oversize payloads are rejected. Browser origins must match `WEB_ORIGIN`; cross-site browser requests are rejected.

202 means the purchase is durably accepted and still processing; follow the Location header or transaction detail while recovery proceeds. 201 means the result was recorded, including `status: FAILED` business outcomes. 200 with `Idempotency-Replayed: true` means the same request was already recorded. A different payload with the same key yields 409. Missing catalog entities return 404. Validation returns 400/422. Internal errors return a sanitized 500.

Metrics are computed from persisted results. Success rate is successful business outcomes / total outcomes. P95 measures local purchase processing, not client round-trip time. No data is represented as zero counts with UI rates shown as unknown. Chart buckets contain observations only; they do not invent traffic for empty minutes.

## Asynchronous notifications

A completed purchase confirms payment/activation outcome and durable notification enqueue. It does not promise that the notification has already been delivered. Kafka or notification-consumer downtime retains the event for retry and does not change the business outcome to Processing. Processing remains reserved for an unfinished purchase workflow. `GET /api/v1/transactions/{id}/notification` returns `PROCESSING`, `AWAITING_DELIVERY`, or `DELIVERED` with a durable receipt time. It returns 404 for an unknown transaction and 503 when delivery status cannot be checked; 503 must not be interpreted as awaiting delivery. The gateway reads only through notification-service's authenticated internal endpoint.

`GET /api/v1/notifications/dead-letters?reason=&cursor=&limit=25` lists at most 50 redacted quarantine records per page. It is shared-runtime data across the two synthetic transaction environments because an invalid event has no trustworthy environment. The response contains source coordinates, reason, retained time, payload size, SHA-256 digest and redacted-topic publication state, plus `more`/`next` for keyset pagination. It never returns the raw payload. Only the fixed reasons `invalid purchase event` and `event identity conflict` are filterable. Invalid filters or cursors return 400/422; an unavailable notification source returns 503.

`POST /api/v1/notifications/dead-letters/{partition}/{offset}/replay` reprocesses the retained original bytes with the current notification consumer. Supply a unique 16–100 character `Idempotency-Key` header and no body. In protected mode, only Engineer and Administrator may call it; unsafe requests also require the configured same-origin header. A new command returns 201 with `DELIVERED`, `ALREADY_DELIVERED`, or an audited `REJECTED` status and fixed reason. Retrying the same key, source and actor returns the identical result with 200; reusing a key for a different command returns 409. Missing sources return 404; unavailability returns 503. The source remains quarantined, and the register exposes its most recent replay status, actor and time. This creates at most one synthetic notification receipt and never starts a new purchase.

`GET /api/v1/notifications/dead-letters/{partition}/{offset}/replays?limit=20&cursor=` lists the append-only reprocessing outcomes for one retained source. All authenticated roles may read it. Pages are limited to 50 and ordered newest first by attempt time and a digest of the private command key; the cursor is bound to the source coordinates. Each item contains status, fixed rejection reason or synthetic transaction ID, actor and time. Raw payloads and idempotency keys are not returned. Missing source coordinates return 404; invalid filters or cursors return 400/422; source failure returns 503.

## Purchase admission

`POST /api/v1/transactions` checks the shared Redis request budget after payload validation and before domain mutation. HTTP 429 includes a rounded-up `Retry-After` in seconds. HTTP 503 means admission could not be checked and no new workflow was started by that request. Keep the same idempotency key when retrying either response. The budget also counts replay requests; background workflow recovery does not consume it.
