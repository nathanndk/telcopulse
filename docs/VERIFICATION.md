# Verification record

## Environment-scoped Operations inbox · 2026-09-28

- The header opens a keyboard-accessible inbox with the current incident count, up to five prioritized active incidents and five recent incident/simulation/deployment audit events for the selected development or staging environment. Records link to their owning investigation and full registers. The two sources retain independent loading, empty and error states; a failed incident source reports an unknown count rather than zero.
- The final web image built and was deployed to the local Compose stack. Frontend lint and typecheck passed. A browser case passed keyboard opening, source links, environment switching, audit-source failure with incident data still visible, and mobile page width. Desktop and 390px screenshots were inspected. The full browser regression passed 51 tests; five protected-auth tests were skipped in default local mode.
- This inbox is a read-only view of existing records. It does not persist per-operator read status, deliver push notifications or replace external paging; those parts of the broader goal remain open.

## Reviewed Kafka dead-letter reprocessing · 2026-09-28

- Migration 027 adds an append-only replay audit keyed by idempotency key. Notification-service revalidates retained bytes with its normal consumer checks. A valid event creates at most one synthetic receipt and delivery fact; a still-invalid event records `REJECTED` without altering the quarantine source. The gateway enforces protected-mode Engineer/Administrator role and same-origin checks, and a notification-specific mutation capability limits the internal service route.
- A fresh disposable PostgreSQL run passed valid replay, eight concurrent same-key calls, an already-delivered second command, invalid and conflicting events, key reuse rejection, source persistence, and append-only audit. The service boundary test passed shared-token denial, Viewer denial, Engineer success, identical retry, actor mismatch and raw-byte non-disclosure. Gateway role/identity forwarding tests passed. Race mode could not compile with the host's limited scratch storage, so these new database cases were run without `-race`.
- Affected Go tests and vet, frontend lint/typecheck, Compose configuration and Helm lint/schema/Secret-scope verification passed. Migration, notification, gateway and web images built and deployed. All health-checked Compose services are healthy. The focused Services browser suite passed 3/3 on the new images, including a confirmed replay with the same key after an uncertain response. The full browser regression passed 50 tests, with five protected-auth cases skipped in the default local demo mode.
- The actual previously quarantined Kafka record at partition 0/offset 156 returned HTTP 201 `REJECTED` with reason `invalid purchase event`; the same idempotency key returned HTTP 200 with the identical result. The live register showed `REJECTED` by `local-operator` and exposed no raw payload. A valid retained event was verified in disposable PostgreSQL; no live repaired event or external notification provider was exercised.

## Operational Kafka dead-letter register · 2026-09-28

- Notification-service now exposes a bounded read-only register with source topic/partition/offset, fixed reason, retained time, payload byte count and SHA-256 digest. A left join to the notification outbox marks the redacted topic fact Published, Pending, or Not tracked for historical rows. Keyset pagination is supported by migration 026; malformed cursors and changed filter scope are rejected. No raw payload is returned.
- A fresh disposable PostgreSQL cluster passed the race-enabled register test for all three publication states, complete pagination without duplicate offsets, redaction and cursor validation. Affected Go tests/vet, frontend lint/typecheck and all four production image builds passed. The migration ran in Compose and `pg_indexes` confirms `notification_dead_letters_recent_idx`.
- The deployed gateway returned the real Kafka poison event at source partition 0, offset 156 with its digest and Published state. The final Services browser tests passed 2/2, including pagination, filter, empty/error handling and mobile viewport. All affected services were healthy after deployment. This register is shared-runtime evidence; malformed events cannot safely be assigned to development or staging from their payload.
- Raw poison records remain restricted to PostgreSQL. The operator reprocessing command is verified in the later section above; editing malformed bytes and an external notification provider remain outside this increment.

## Kafka poison-event dead-letter topic · 2026-09-28

- Notification-service now commits a redacted `telcopulse.notification.dead-letter.v1` outbox fact in the same transaction as raw quarantine. Source topic/partition/offset form stable identity; the Kafka payload carries reason, size and SHA-256 digest but no raw event bytes. The existing service-owned relay publishes it with at-least-once retry.
- A fresh disposable PostgreSQL cluster passed three race-enabled neighboring cases: notification outage recovery, new dead-letter outbox retry/deduplication/redaction, and delivery rollback on event failure. Affected Go tests and vet passed.
- The rebuilt notification-service ran against the local Kafka broker. `tests/integration/kafka_dead_letter.py` injected a unique malformed purchase record, found its raw quarantine entry, waited for the matching outbox publication and consumed the redacted event from the actual dead-letter topic. Event `notification.dead-letter:telcopulse.purchase.completed.v1:0:156` contained the expected digest and no raw marker. Operator replay is verified in the later section above; the UI register is verified there as well.

## Asynchronous notification outcome · 2026-09-28

- Notification-service now serves an authenticated read of its durable delivery receipt. The gateway first resolves the transaction, then returns `PROCESSING`, `AWAITING_DELIVERY`, or `DELIVERED` with the receipt time; a missing transaction is 404 and a failed source read is 503. The transaction page polls pending states and distinguishes unavailable status from a missing receipt.
- Go tests and vet passed for the affected services, including gateway projection cases for malformed/unknown transactions, pending, awaiting, delivered, and upstream failure. Frontend lint, typecheck and production image build passed. Notification, gateway and web images rebuilt and started healthy in Compose.
- The live Kafka recovery integration passed: a stopped notification-service produced 503 instead of a false awaiting state; a paused broker produced `AWAITING_DELIVERY`; restoring either dependency yielded one durable delivered receipt for the original transaction. The full purchase browser suite passed 5/5, including the rendered delivered state. All local services were healthy afterward.
- The pinned Helm verifier passed strict lint and Kubernetes schema checks for development, staging, production, vendor and bootstrap renders. This is synthetic receipt persistence; it does not prove an external SMS/email provider delivered a message.

## Linked failed synthetic purchase replay · 2026-09-28

- Failed purchases can be replayed from the simulator result or transaction detail after confirmation. A new attempt gets a distinct transaction, trace and idempotency key, while `replay_of` links it to the unchanged source. The gateway checks source status, environment, customer, package and payment method before calling downstream services.
- A real disposable PostgreSQL test passed for valid replay, persisted lineage, independent outbox/audit records, unchanged failed-source balance, idempotent network retry, and rejection of missing, mismatched or healthy sources. Request validation and HTTP error mapping tests passed. The affected Go packages passed tests and vet; frontend lint and typecheck passed; gateway and final web images built.
- After recovering an unresponsive local Docker Desktop daemon without deleting named volumes, the final Compose web image ran healthy and all five purchase Playwright cases passed against `http://localhost:3000`, including the linked replay browser flow.
- This action creates a new synthetic purchase and can debit synthetic balance if successful. It does not replay Kafka dead letters or retry real customer payments.

## Sustained synthetic purchase recovery sample · 2026-09-28

- Added a partial PostgreSQL index on terminal purchase-workflow completion time; the migration ran successfully in Compose and `pg_indexes` confirms `purchase_workflows_terminal_completed_idx` exists.
- The recovery endpoint uses the incident's environment and latest audited Monitoring transition, then counts completed synthetic purchases in adjacent rolling five-minute windows from one repeatable-read snapshot. It requires five outcomes per window and 99.9% success in each before reporting that the *sample* meets target. A real PostgreSQL test confirms completion-time counting, environment isolation and a recent failed outcome changing the assessment to `still_failing`; its fixtures now roll back in one transaction without bypassing the append-only audit trigger.
- The Monitoring detail page shows the live counts, sample status and evaluation time, including explicit collecting/low-traffic unknown states. A browser test confirmed the new endpoint returns `collecting` for a fresh Monitoring incident and that reviewing a target-meeting snapshot adds audited metric evidence without auto-resolving it.
- Full Go tests and vet, frontend lint/typecheck/build, and the affected Compose image builds pass. The index migration and updated gateway/incident/web containers are running; all health-checked services report healthy. The complete incident browser suite passes 18/18. The full browser suite passes 42 cases, with five auth/role tests skipped in local mode as configured. After the final Monitoring-only panel scope and rollback-safe query refactor, the two directly affected browser flows and full Go checks passed again on the rebuilt containers.
- This is a synthetic purchase sample, not an automated claim about external traffic, every service dependency, or all affected customers. A full sustained degradation→mitigation→recovery demonstration and external evidence-source verification remain open.

## Reviewed recovery validation · 2026-09-28

- Monitoring → Resolved now requires an operator recovery observation, HTTP(S) evidence link and observed time after detection. The incident service records the authenticated validator and validation time in the same revision as the resolution and audit entry. Reopening clears the current validation; the earlier snapshot remains in audit history. Structured postmortems freeze the recovery record when present.
- `go test ./...` and `go vet ./...` pass. Targeted incident tests pass against the running PostgreSQL database, including structured postmortem generation and the authenticated recovery stamp.
- Frontend lint, typecheck and production build pass. Incident and web Compose images build; all 16 services are running, and health-checked services report healthy.
- The 17-case incident Playwright suite exposed a recovery-time input precision mismatch and a test fixture that resubmitted a read-only recovery field. Both were corrected; the complete suite then passed 17/17 against the updated Compose stack.
- Recovery is an operator-reviewed, sourced observation. The platform does not yet automatically compare sustained post-mitigation business SLI against a pre-incident baseline or verify that an external evidence URL still serves its content.

## Phase 1 · 2026-09-27

Passed on the local Go/PostgreSQL/Next.js stack:

- Go compilation, `go vet ./...` and `golangci-lint run` (zero issues).
- `go test -race ./...` with a real isolated PostgreSQL test database.
- Validation and MSISDN masking unit cases.
- API content type, idempotency key, origin, unknown-field, trailing-JSON and environment rejection cases.
- Successful purchase: one debit, payment, entitlement and audit entry.
- Replayed key returns the same result; changed input returns conflict.
- Eight concurrent identical requests create a single purchase/debit.
- Four concurrent distinct purchases cannot overdraw a subscriber balance.
- Insufficient balance persists a failed outcome and creates no entitlement.
- Overview counts business outcomes; list filters and environment isolation work.
- Frontend ESLint and TypeScript checks.
- Next.js production build with webpack; all seven routes generated successfully.
- Four Playwright E2E tests: successful purchase/investigation/search, insufficient balance/no activation, mobile navigation/no document overflow, API error/retry state.
- Visual inspection at 1600px desktop and 390px mobile for overview/simulator.

Also passed against the Docker Compose stack:

- Both multistage images built successfully (Go 1.25 and Node 22).
- PostgreSQL, gateway and frontend report healthy.
- All four Playwright E2E tests pass at http://localhost:3001.
- Application runtime users confirmed non-root (gateway UID 10001, web UID 1000).
- Only frontend loopback port 3001 is published; API and PostgreSQL have no host ports.

Compose validation caught and fixed a host access issue: the frontend requires a host-facing bridge in addition to the internal service network. Database and API remain exclusively internal.

## Phase 2 · 2026-09-27

Implemented independent subscriber, package, payment and notification HTTP services, additive schema migration, generated service authentication and a durable purchase coordinator. Removed the gateway's duplicated local catalog/payment/activation implementation.

Verified natively with real PostgreSQL and HTTP service handlers:

- `go test -race ./...`, `go vet ./...` and `golangci-lint run` pass from `services/`.
- Full purchase produces one reservation, activation, notification, completed transaction and audit entry.
- Same-key replay and conflicting-input rejection work across the coordinator boundary.
- Concurrent distinct purchases cannot overdraw an account.
- A committed payment with a lost HTTP response resumes without a duplicate debit.
- Definitive activation failure releases the reservation and creates no entitlement.
- Notification outage is retried by the durable recovery worker.
- Pending transactions are visible but excluded from completed business metrics.
- HTTP 202 includes Processing state and a transaction Location header.
- Activation rejection is persisted and remains rejected after catalog changes.
- All four browser E2E tests pass against the native gateway plus four separate service processes.
- A live notification-process suspension test confirmed HTTP 202, the visible Processing heading, and automatic success with the same transaction ID after process resumption.
- Frontend lint, typecheck and the updated webpack production build pass.

Container upgrade verified after storage recovery:

- Sequential Compose build completed successfully with the shared Go compiler cache.
- Migration container exited successfully; PostgreSQL, all four domain services, gateway and web report healthy.
- All four Playwright tests pass against the production frontend at http://localhost:3001.
- Only the frontend loopback port is published.

The first parallel build exhausted host storage and caused Docker I/O errors. Docker was restarted after freeing generated frontend caches; application data and volumes were preserved. Sequential builds and a shared compiler cache reduce peak storage use.

## Kafka notification increment · 2026-09-27

- Versioned purchase event and outbox row commit atomically with the purchase outcome and audit record.
- Go race/integration tests pass against PostgreSQL and a real Apache Kafka 4.1.1 broker, including rollback, retries, duplicate delivery, conflicting identity and poison-record quarantine.
- `go vet` and `golangci-lint` pass with zero issues.
- Frontend lint, typecheck and webpack production build pass.
- Updated gateway, notification and frontend container builds pass; all four browser E2E tests pass against the asynchronous backend.
- Live Compose notification-stop test: purchase completes and publishes while the consumer is down; restarting the consumer produces exactly one receipt.
- Live Compose broker-pause test: purchase completes, failed publication attempts persist, and unpausing the broker delivers exactly one receipt.
- The first broker-pause check exposed unbounded ambiguous idempotent producer writes. Bounded non-idempotent publication plus database deduplication corrected it; the repeated real outage test passes. This is at-least-once event transport, not global exactly-once delivery.
- Recovery smoke test is reproducible with `WEB_PORT=3001 python3 tests/integration/kafka_recovery.py`.

## Redis increment · 2026-09-27

- Redis catalog cache and a shared atomic fixed-window purchase budget are implemented.
- All Go race/integration tests pass with real PostgreSQL, Kafka and Redis. `go vet` and `golangci-lint` pass.
- Concurrent requests admit exactly the configured count; a second Redis client observes the same budget. Window expiry reopens admission.
- Corrupt catalog entries fall back; cached display prices cannot change purchase prices. Upstream catalog failures remain errors.
- HTTP tests verify 429/Retry-After and 503 responses do not invoke the purchase repository.
- A standalone native gateway using the real domain services verified successful purchases, 429 throttling, and a paused Redis process: catalog fallback remained 200, purchase admission returned 503, and the combined outage requests completed within three seconds. Redis was resumed afterward.
- Gateway native binary build passes. The Redis-enabled container build is **not verified**: host storage was exhausted during dependency-layer creation and Docker reported a BuildKit metadata I/O error. The Redis image pull did not run. Obsolete TelcoPulse compiler caches were removed, preserving application volumes; additional host space is needed before reliable container upgrades.
- No frontend files changed in this increment.

## Redis deployment and domain events · 2026-09-27

The previous storage block is resolved for this increment. Docker was stopped/restarted after its confirmed storage failure; available host space recovered to roughly 4.8 GB. Shared Go module and compiler caches now avoid repeated dependency layers. The Redis image and gateway/payment/notification/migration builds completed successfully. Existing application volumes were preserved.

- Redis, Kafka and application containers are healthy; additive migration 006 completed.
- `tests/integration/redis_recovery.py` passes on Compose: cached catalog reads, bounded fallback while paused, 503 without a new workflow, and successful retry with the same key after resumption.
- Payment transitions and synthetic notification receipts commit into separate service-owned outboxes. Integration tests force event insertion failures and prove debit/receipt/inbox rollback.
- All Go race tests, vet and lint pass against PostgreSQL, Redis and Kafka. Real broker tests consume RESERVED, CONFIRMED and DELIVERED facts and verify correlation, sequence and causation.
- Updated Kafka recovery smoke tests pass with the consumer stopped and the broker paused; all corresponding payment/purchase/notification events publish after recovery, without duplicate logical receipts.
- No frontend source changed; all four browser E2E tests pass against the deployed Redis/event-enabled stack at localhost:3001.

## Metrics instrumentation · 2026-09-27

- HTTP counters/histograms, commit-observed business outcomes, process/runtime, pool usage, outbox backlog and broker lag collectors are implemented.
- All Go race/integration tests, vet and lint pass. Tests confirm that replays do not double-count business success, business failures count independently of HTTP status, and raw IDs/query strings do not become labels.
- Prometheus configuration and all seven recording/alert rules pass standalone `promtool` validation. Rule fixtures pass for degraded, healthy and idle business traffic. The official 3.13.3 Darwin ARM64 tool archive was verified against its published SHA-256 before execution.
- Monitoring images were downloaded, but container startup encountered disk exhaustion and a metadata I/O error. Docker was recovered and approximately 1 GB of inspected obsolete task caches removed.
- The initial config mount was denied by Docker's macOS Documents access. Configuration now ships in dedicated images instead of bind mounts.
- Docker recovered; instrumented service and monitoring images built successfully. Packaged Prometheus configuration and rule fixtures pass containerized promtool checks. All five service scrape targets are up.
- Live metrics integration passes: six business failures fire the success-rate alert, replay is excluded, stopping the notification consumer increases measured broker lag, and restarting it returns lag to zero.
- The pinned Grafana ARM64 image contains an empty /run.sh. The config image launches the working server binary directly with explicit persistent data/provisioning paths. Grafana reports database health OK, serves the provisioned 12-panel dashboard, and renders measured series in Chrome. All panel queries execute successfully against Prometheus.
- All four application browser E2E tests pass against the instrumented Compose stack.
- Reproducible live checks are in `tests/integration/metrics.py`. OpenTelemetry and vendor integrations remain unfinished.

## Not yet complete

The full goal remains active. Distributed tracing, vendor observability, incident operations, failure simulation, infrastructure, enterprise features and the complete flagship incident demonstration remain incomplete. HTTP and PostgreSQL OTel spans and OTLP export are implemented and tested; the local Jaeger backend is now verified; Kafka propagation and recovery links remain incomplete; Prometheus/Grafana are running locally; Splunk and Dynatrace ingestion remain unimplemented/unverified. Apache Kafka now transports purchase-completed, payment-state and notification-delivered events. No production authentication or RBAC is claimed.

## Environment notes

Native development API: loopback port 8080. Native frontend: localhost:3000. Temporary verification PostgreSQL: loopback port 55432, with separate application and disposable test databases. Compose verification uses localhost:3001 to avoid conflicting with the native server.

Turbopack production compilation failed because its CSS worker could not bind an IPC port in this workstation environment. Webpack production compilation succeeds; `npm run build` uses that supported backend.

## Route propagation correction · 2026-09-27

Inspection found that timeout middleware cloned the request before routing, losing the matched pattern seen by outer metrics middleware. Both gateway and domain timeout middleware now propagate the router-selected pattern back after handling. Regression tests cover the gateway timeout and nested authenticated domain router, including deadline preservation and exclusion of raw IDs/query strings. Targeted race tests, repository-wide vet and lint pass. All five containers rebuilt successfully, all four browser tests pass, and `tests/integration/metric_routes.py` confirms real gateway and subscriber endpoint patterns in Prometheus after purchase traffic.


## HTTP/PostgreSQL OpenTelemetry · 2026-09-27

- All five Go services install an OTel SDK provider with optional bounded OTLP/HTTP batch export and graceful shutdown. Internal RPC propagates W3C context. New purchase correlation IDs use the initial active trace.
- An HTTP receiver test decodes actual OTLP protobuf exports and proves gateway/server → RPC/client → downstream/server parent relationships and trace continuity. It checks that private identifiers, payloads, query values and credentials are absent. Additional tests cover malformed incoming context, server errors and probe exclusion.
- PostgreSQL tracing tests execute successful and failing queries against the real disposable PostgreSQL database. Query spans retain the parent, mark failure, omit SQL/arguments and avoid disconnected root spans for background polling.
- The native PostgreSQL 15 test cluster had stopped during earlier disk exhaustion. Its matching version was restarted successfully; the full Go race/integration suite then passed against PostgreSQL, Redis and Kafka. Vet and lint pass.
- All instrumented service images build and start successfully. All four browser E2E tests pass. `WEB_PORT=3001 python3 tests/integration/trace_logs.py` verifies that a real purchase's trace ID appears in correlated gateway, subscriber, package and payment completion logs.
- Compose passes through optional `OTEL_EXPORTER_OTLP_ENDPOINT`; no backend is configured by default yet. Test-receiver export is verified, but deployed trace storage/viewer, Kafka spans/links, recovery links and vendor ingestion are not claimed. Workflow display step IDs are not yet exported-span references.


## Local Jaeger trace retrieval · 2026-09-27

- A pinned Jaeger 2.21.0 config image builds and starts successfully. Its OTLP/HTTP receiver is internal-only and the viewer binds to localhost:16686.
- Compose services now export to Jaeger by default (an explicit empty endpoint disables export). Local storage is ephemeral, capped at 5,000 traces with a 256 MiB memory limiter and 384 MiB container limit; production trace persistence/retention is not claimed.
- `WEB_PORT=3001 python3 tests/integration/traces.py` passes against the live stack: a synthetic purchase yields 48 spans across gateway, subscriber, package and payment services. All child references resolve, and PostgreSQL spans are present.
- Chrome renders the actual trace timeline with four services, 48 spans and database operations; visual inspection passed. Screenshot: `/private/tmp/telcopulse-trace.png` (temporary local evidence).
- Disk space was 380 MiB at the start. Removed only the identified TelcoPulse compiler cache (399.6 MB); image build succeeded with approximately 620 MiB remaining. More free space is still needed for sustained builds. No application volumes or user files were deleted.
- Kafka notification spans, durable context propagation, workflow recovery links and vendor ingestion remain unfinished.

## Durable Kafka trace context · 2026-09-27

- Additive migration 007 stores traceparent separately in all three service-owned outboxes. Payload schemas and inbox deduplication remain unchanged.
- Every publish attempt records a producer span and injects its context into Kafka record headers. Notification processing records consumer spans, including generic failure/quarantine status; delivered events preserve that consumer context.
- The full Go race suite passes against PostgreSQL, Redis and Kafka. A new database integration test separately proves detached publication after the original request ends, failed publication/retry with distinct spans, correct consumer parentage, and one logical receipt under duplicate processing. Vet and lint pass.
- All five images rebuild and start successfully; migration 007 applies in Compose. Jaeger retrieves a real 68-span trace across all five services, including all event producers and notification consumption.
- Consumer-stop and broker-pause recovery tests pass after instrumentation: purchases succeed, events publish after recovery, and one logical notification receipt remains. Both interrupted services are restored.
- Only inspected TelcoPulse compilation-result caches were pruned; source, running images and application volumes were retained. Available disk space recovered to about 1.6 GB during the build.
- Workflow recovery links, transaction UI links to exported spans, vendor ingestion and incident operations remain incomplete; the full goal stays active.

## Linked workflow recovery attempts · 2026-09-27

- Migration 008 stores originating span context on durable workflows. `purchase.resume` spans retain the active request parent or start an independent background trace, and link to the original request span. Transaction ID, environment and outcome support investigation.
- The full Go race/integration suite passes against PostgreSQL, Redis and Kafka. The new lost-response test proves that background recovery has its own trace and original-span link without a duplicate debit. Durable Kafka retry tests now assert publication follows the workflow attempt span. Vet and lint pass.
- Service images build and migration 008 applies successfully. The live test stops payment, receives a pending purchase, restores payment in finally, and observes autonomous success.
- The initial verifier used a search route absent from this Jaeger release. Inspection of the running viewer identified the v3 trace-summary route; the corrected test retrieves the recovery trace and verifies its FOLLOWS_FROM reference matches both the original trace and span IDs. `tests/integration/trace_recovery.py` passes; payment is restored.
- Operator-facing trace navigation, exported-span references for transaction steps, vendor integrations and the full incident workflow remain unfinished.

## Operator trace navigation · 2026-09-27

- Transaction details link to the original trace and transaction-filtered workflow attempts, using request-time TRACE_VIEWER_URL configuration. Unsafe/credentialed destinations are rejected, and an unconfigured deployment has an explicit state.
- Workflow display identifiers are now labeled Stage reference. The page explains export delay, ephemeral retention and the two-day attempt-search window.
- Native production build passes; both new browser tests pass against real Jaeger trace/detail and search results. The browser test waits for actual asynchronous export before opening the viewer. Final source (including content inset and availability copy) passes both tests, lint and typecheck in the local development preview on localhost:3004, backed by the Compose API on localhost:3001. Desktop/mobile visual inspection passed.
- **Frontend container deployment remains incomplete.** The container build hit EROFS and Docker metadata I/O failure under disk pressure. The Dockerfile now keeps transient npm dependencies/cache in tmpfs, but the complete image build is not yet verified. Removed only generated native Next cache/standalone output, restarted Docker, and successfully restored all Compose services from existing images. Compose therefore still serves the previous UI.
- Free disk space was roughly 355 MB after recovery; free at least 2 GB before retrying the frontend image build. Application volumes and source files were retained. The full goal remains active; this does not complete vendor integration or incident operations.

## Trace navigation deployed · 2026-09-27

- Disk revalidation found 1.9 GB free, permitting a controlled retry. The frontend image now builds successfully with transient npm dependencies in tmpfs; standalone output is copied into the runtime image and serves correctly.
- Recreated the Compose web service. All application services are healthy, with Prometheus, Grafana and Jaeger running. The updated trace-navigation UI is now deployed at localhost:3001; this supersedes the previous pending-deployment note.
- The first browser run encountered ENOSPC while writing diagnostic trace output. Its trace-navigation test also exposed partial OTLP arrival: a trace can exist before its gateway root span arrives. Updated the readiness check to inspect actual span content instead of accepting HTTP 200 alone.
- Removed only the identified frontend compilation cache, stopped temporary preview processes on ports 3003/3004, and reran the unchanged six browser scenarios with diagnostic trace recording disabled to conserve disk. All six pass against the deployed stack, including opening the actual original trace and filtered workflow-attempt search.
- Free disk space after cleanup was approximately 591 MB. No application volumes or source files were removed. Further substantial builds still require more space.

## Incident backend foundation · 2026-09-27

- Migration 009 adds service-owned incident records and append-only audit entries. The independent incident service provides internal create/list/detail/update endpoints with service-token protection, local-only startup, bounded strict JSON, idempotent creation and version-checked updates.
- All eight lifecycle states are implemented with ownership/evidence gates and explicit reopening behavior. Fields cover impact, nullable measurements, deployment reference, evidence, RCA, mitigation, resolution, postmortem notes and action items.
- Full Go race/integration tests, vet and lint pass. PostgreSQL tests verify replay/conflict, one winner under concurrent edits, consistent audit snapshots, rejected audit edits/deletes and rollback when audit insertion fails. HTTP checks cover authentication, body bounds, filters and rejection of client-supplied actors.
- An initial scripted Compose edit was rejected by automatic review over a suspected service replacement. An explicit additive patch retained subscriber-service; Compose validation confirms both services. No rejected change ran.
- Incident, migration and Prometheus images build successfully. Go compiler intermediates now use tmpfs in the shared service image build to reduce disk writes; this trades persistent compilation caching for lower local disk use.
- The deployed internal API smoke test (`WEB_PORT=3001 python3 tests/integration/incidents.py`) advances a synthetic incident through all eight states, verifies eight audit entries and lifecycle timestamps, and confirms a stale edit returns 409. Migration 009 is applied; incident-service is running.
- All six Go services are up in Prometheus, and packaged promtool configuration/rule checks pass. The existing subscriber service is retained.
- Gateway routes, UI, automatic alert ingestion, history pagination, authenticated human identity/RBAC and the full incident demonstration remain incomplete. The fixed local-operator audit identity is not represented as production authentication.

## Incident gateway integration

Deployed the rebuilt Go gateway with the incident-service URL; updated native startup to run the incident service on port 18085. Gateway boundary tests passed under the race detector, including origin rejection, credential isolation, status preservation and masked upstream failures. Full Go race/integration suite, go vet and golangci-lint passed after the shared RPC refactor.

`PUBLIC_INCIDENT_API=1 WEB_PORT=3001 python3 tests/integration/incidents.py` passed through the deployed frontend rewrite and gateway: creation/replay, mismatched-key conflict, filtered list, invalid limit/transition, all eight states, stale-version conflict and eight audit entries. `WEB_URL=http://localhost:3001 npx playwright test --trace off` passed all six existing browser tests, including successful/failed purchases and actual Jaeger navigation. Operator UI, alert ingestion and human RBAC remain pending.

## Incident pagination

Bounded detail history to ten ordered revisions with `history_after`, `history_more` and `history_next`. Lists now expose compact summaries and a filter-scoped keyset cursor using the actual PostgreSQL timestamp plus ID. Real PostgreSQL race tests verified a 12-revision first read, an intervening edit, a second page containing exactly revisions 11–13, an empty terminal page, identical-timestamp list boundaries, and rejected negative/malformed/wrong-environment cursors. Existing incident atomicity and API tests pass. Incident/gateway race tests, go vet and golangci-lint pass.

Rebuilt and deployed both affected images. The public Compose smoke passed all eight lifecycle states, conflicts and validation, then additional edits verified history pages of 10 and 1, distinct list pages, rejected malformed cursors and summary-only list output through the frontend and gateway. No migration was required. List pagination reflects live ordering, not a frozen cross-request snapshot; operators must refresh the first page for records updated ahead of their cursor.

## Incident operator read interface

Deployed `/incidents` and `/incidents/[id]` with Zod-validated API responses, environment/state filters, cursor navigation, investigation details, evidence links, action items and paged immutable history. Navigation includes desktop, mobile and command palette entries. Unknown measurements remain explicit. Evidence links allow only HTTP(S) without embedded credentials.

Typecheck, ESLint and the production Docker build pass. All six existing purchase/navigation/trace browser tests passed. Both new incident tests pass: real API creation and eleven revisions, register-to-detail navigation, history pages of ten and one, evidence link, unknown measurements, 390px viewport overflow check, and API failure/retry recovery. Desktop and mobile screenshots were visually inspected. The first fixture used an invalid evidence kind and was corrected to `metric` before passing.

This interface is currently read-only. Creation/edit/transition controls, conflict recovery UX, richer evidence correlation and automatic alert ingestion remain pending; local operator access is not human authentication.

## Incident editing and lifecycle controls

Deployed a React Hook Form/Zod incident editor for title, severity, owner, impact, lifecycle state, RCA, mitigation, resolution and postmortem notes. Every save requires an audit note. The editor retains its opening revision, preserves existing evidence/action items/measurements, and uses the backend version check. API errors now retain HTTP status so 409 conflicts preserve the draft and disable saving until explicit reload. Detail polling/window-focus refresh is disabled so background refresh cannot replace the open form.

Typecheck, ESLint and production Docker build passed. All nine browser tests passed against Compose, including each lifecycle transition through Postmortem, required owner/audit-note validation, preservation of evidence/action items and a measured affected-user count, a real concurrent API edit followed by stale-draft rejection and explicit reload. Mobile editor screenshot was inspected and the 390px overflow check passed. Existing purchase, failure-state and Jaeger navigation tests remain green.

Manual creation UI, evidence/action-item/measurement editing, richer audit diffs and automated alert ingestion remain outstanding. Operator identity remains local-only pending authentication/RBAC.

## Manual incident creation

Deployed the register creation dialog with title, service, severity, optional owner and impact in the current environment. Unknown measurements remain unset. A submitted command/key is retained in component memory on uncertain errors, locks its fields, and supports exact retry after closing/reopening the dialog. Known validation errors unlock correction.

Typecheck, lint and production Docker build pass. All four incident browser tests pass, including a real backend commit followed by an intentionally aborted browser response. Reopening and retrying sent the same key twice, navigated to the original incident ID and retained exactly one audit entry/version. Existing investigation, error recovery and full lifecycle/conflict tests pass. The 390px creation screenshot was visually inspected and the overflow check passed. Pending requests do not survive page reload/navigation; the form explicitly asks the operator to keep the page open until confirmed.

## Incident impact measurements

Deployed editing for nullable affected-user/transaction counts, error/success rates (fractions 0–1), latency in milliseconds and related deployment references. Counts require nonnegative safe integers. Empty/null inputs remain unknown rather than becoming zero. All changes retain revision checks and audit notes.

Typecheck and lint passed. The first Docker build encountered confirmed metadata I/O/read-only filesystem errors under host disk pressure. Removed only known generated Next dev output and identified TelcoPulse build-result caches; runtime images and application volumes were preserved. A subsequent build succeeded, but the runtime suffered a second confirmed read-only filesystem failure. After Docker recovery and removal of that build's temporary cache, the existing runtime images started successfully.

All eleven browser tests then passed. New coverage verifies invalid rates are rejected, measured zero and 37 affected transactions persist, 5%/95% rates and 4200 ms latency persist, deployment reference persists, audit before/after distinguishes unknown from zero, and clearing fields restores null. Existing lifecycle tests also verify untouched unknown measurements remain null. Purchase, incident, lost-response retry and Jaeger navigation regressions pass. Storage remains constrained; avoid accumulating compiler/build-result caches.

## Frontend build disk footprint

Moved the complete transient Next `.next` directory onto a BuildKit tmpfs mount alongside dependencies/npm cache. The build copies only standalone server files and static assets to `/output`; the runtime stage copies those explicit outputs. The production build passed and BuildKit measured its retained result at 63.35 MB, down from the previous 260.9 MB (approximately 76% less). Deployed the new image and all eleven browser tests passed, including incident creation/recovery, lifecycle conflicts, impact measurements, purchases and real Jaeger navigation. No Docker restart or application-volume deletion was needed for this build. This trades additional build memory for lower persistent disk usage; runtime memory settings are unchanged. Host disk space remains limited.

## Evidence and action-item editing

Deployed bounded collection controls for evidence kind/summary/optional URL and action title/owner/optional deadline/completion. Draft removal takes effect only on audited, version-checked save. Null collections normalize to empty lists. Form validation rejects unsafe/credentialed URLs and unowned actions. Incident response validation now accepts explicit timestamp offsets, matching Go's preservation of supplied deadline timezones. Collection UI is separated into its own component.

Typecheck, ESLint, production Docker build and all twelve browser tests pass. New real-API coverage verifies unsafe URL and missing-owner validation, persistence, +07:00 deadline round-trip, evidence removal, completion, and immutable before/after audit values. Mobile editor screenshot was inspected. Existing purchase, trace, incident lifecycle/conflict, impact and lost-response retry tests pass.

After interruption, old process handles were absent. The resumed build stalled in `docker-credential-desktop get`; it was explicitly canceled and completed using an isolated `/private/tmp/telcopulse-docker-public` configuration with no credentials for public-image retrieval. User Docker settings were unchanged. The final image is deployed at port 3001.

## Automatic alert-to-incident detection

Deployed an incident-service worker that polls the configured Prometheus alerts API every ten seconds and creates incidents for the five known firing rules. Source + complete labels + UTC activation time form a durable episode key. First detection is audited as `prometheus-alert`; repeated observations return the latest incident without overwriting operator edits or adding history. Customer measurements remain unknown. The worker bounds response size/count, request duration and redirects and logs failures.

Race tests for incident/alert packages and real PostgreSQL deduplication passed after restarting the stopped disposable test database. Verified operator edits survive repeated observations and viewer-configuration changes, later activation creates a distinct episode, timezone-equivalent activation timestamps share identity, invalid scope/rules are rejected, and oversized/malformed responses fail. Go vet and golangci-lint pass; the incident image built and deployed successfully.

`WEB_URL=http://localhost:3001 WEB_PORT=3001 python3 tests/integration/alert_incidents.py` passed against the live Compose stack. It stopped payment, waited for Prometheus ServiceUnavailable to fire, observed one new persisted incident, checked repeated polls retain exactly one creation audit entry, restored payment in a finally block, verified the source alert cleared, and confirmed the operator incident stayed Detected. Payment service was restored. Snapshot polling can miss episodes entirely within an ingestion outage; durable notifications and HA alert grouping are not claimed.

## Active incidents in the operations overview

Deployed an environment-scoped incident overview endpoint and dashboard integration. One database snapshot returns active and SEV-1/2 counts plus five severity-prioritized summaries. Resolved/Postmortem records are excluded. The dashboard links to persisted incident details, refreshes both overview sources, and renders unavailable data as unknown rather than zero.

Frontend typecheck and lint, targeted incident/alert Go race tests with real PostgreSQL, go vet and golangci-lint passed. Backend coverage verifies environment isolation, counts, severity ordering, lifecycle exclusion and invalid environments. All thirteen browser tests passed against the deployed Compose stack; the new test checks persisted incident priority, KPI counts, detail navigation and forced-503 recovery without a false zero. The desktop overview screenshot was visually inspected.

Initial builds exhausted host storage and Docker reported a metadata I/O error. Recovery removed only generated service binaries and explicitly identified TelcoPulse build-result caches, preserving application volumes. The first frontend image had an invalid package manifest after that failure; a fresh no-cache build succeeded, both runtime package manifests were parsed successfully, HTTP readiness passed, and the complete browser suite then passed. Host storage remains constrained; run heavy builds sequentially and retain the broader request for additional free disk space.

## Live service HTTP health

Added a fixed-query Prometheus adapter in the Go gateway and connected both the overview and Services page. The API reports shared-runtime scope, a common evaluation timestamp, five-minute HTTP RPS/5xx/P95, and scrape availability. Unknown, idle, failed-scrape and degraded states are explicit. HTTP health does not replace business success or Kafka delivery measurements.

Adapter and gateway race tests passed, including missing/stale/non-finite samples, malformed/oversized responses, partial-query warnings, configuration validation and status thresholds. Go vet and golangci-lint passed. Frontend typecheck/lint and sequential production builds passed. The live integration test stopped payment, observed its actual Prometheus scrape become Critical, restored payment in a finally block, and verified recovery. All fourteen browser tests passed with real telemetry, API failure/retry and mobile overflow coverage. Screenshot review identified long status text consuming the dashboard table width; explanations were subsequently wrapped and Degraded receives amber styling.

The final layout/style image was rebuilt and deployed. Typecheck/lint and the targeted service-health browser test passed again; the added desktop assertion verifies the entire table fits its overview panel, while the mobile page remains free of page-level overflow. The final overview screenshot was visually inspected with RPS, HTTP 5xx and P95 all visible.

## Incident telemetry evidence capture

Incident details now show current measurements for the affected service using the shared service-health contract. Operators can freeze evaluation time, measurement window, scope, availability, RPS, HTTP 5xx and P95 into a reviewed metric-evidence draft. Existing version checks, collection limits and mandatory audit notes govern saving. Captures do not overwrite incident impact, lifecycle or RCA. An open draft remains mounted when telemetry polling fails; failed telemetry disables new captures.

Typecheck, lint and the production frontend build passed; the image is deployed. All fourteen existing browser tests passed. The new capture test initially used a role locator that excluded the modal-hidden background panel; correcting that locator allowed it to pass. It verifies a real persisted staging incident, shared-runtime labeling, no write before review/save, unchanged frozen values during a polling outage, evidence persistence, immutable before/after audit snapshots, retained null impact measurements, unchanged Detected state, disabled capture during outage and recovery afterward. The resulting incident screenshot was inspected.

## Controlled payment-decline simulation

Deployed migration 010, simulation-service, gateway start/list/stop routes and payment decision integration. Runs are environment-scoped, percentage-bounded, automatically expire and require a reason. Exact creation retries preserve the original deadline. Explicit stop is idempotent and audited. Durable per-transaction decisions preserve both selected failure and no-failure outcomes across run changes. Payment applies injection only to a new reservation, persists its normal FAILED result and outbox event, and leaves the balance unchanged. Existing reserve retries and confirm/release bypass decision lookup.

Automatic approval review rejected an initial test command because its database-reset fixture targeted an existing database without proof of disposability. Created the new empty `telcopulse_simulation_verify_20260927` database and ran verification there. The actual simulation test then passed under the race detector, covering concurrent decision deduplication, command replay/conflict, environment isolation, bounds, immutable start/stop audit, expiry, balance preservation, outbox emission and purchase retry/recovery. The full workflow regression suite passed in that dedicated database. Gateway race tests verify origin rejection, credential isolation, query filtering and invalid input. Go vet and golangci-lint report no issues.

Sequential simulation/payment/gateway/migration and Prometheus builds succeeded and deployed. Targeted cleanup reclaimed inspected obsolete frontend build caches; runtime images and application data were retained. Promtool validates the deployed configuration and rules, and the new simulation scrape reports up=1.

`WEB_URL=http://localhost:3001 python3 tests/integration/simulations.py` passed: run `SIM-1ba30bb7b5722ee1dcc88fd6` produced five staging failures, preserved a development purchase, stopped explicitly, allowed a new staging success and retained the original failed outcome on retry. The run was stopped in the test's finally block. Prometheus reported five staging business failures and fired BusinessSuccessRateLow; incident `INC-3f970b05681ad5843b9b00ca` was automatically created in Detected. Jaeger for failed transaction `TXN-864e7c031169e84871827e00` contains simulation-service along with the purchase services. Payment logs carry the run and transaction IDs. All fifteen browser tests passed against the deployed stack.

This verifies the initial API-driven business-failure scenario, not the complete requested demonstration. UI controls, simulation audit browsing, richer incident/run correlation and the remaining failure scenarios, vendors, RBAC and deployment platform are still pending. The alert's five-minute window can remain degraded after mitigation; incidents require operator resolution.

## Simulation operator controls and run history

Deployed typed simulation contracts, reviewed start/stop dialogs and an environment-scoped latest-20-run table in Customer Simulator. Start bounds/reasons are validated; active runs and history errors disable new starts. Uncertain creation retains the exact command/key and original environment across dialog close/reopen and global environment changes. Stop requires a reason and keeps the selected run ID. The UI explicitly documents that pending creation recovery does not survive navigation/reload.

Typecheck, ESLint and production Docker build passed. Sixteen browser cases passed in the initial full run; the remaining new case found an ambiguous test locator matching both the global and disabled form environment selectors. Scoping it to the header fixed the test. Both simulation tests then passed against the deployed image: committed response loss and same-key retry produce one run, staging scope persists through an environment switch, 100% injection creates a real failed purchase, explicit stop permits a new successful purchase, history outage disables new runs, and retry restores the panel. Each test-owned run is stopped in finally. All seventeen cases passed across those runs. Mobile screenshot was inspected and page overflow check passed.

## Simulation audit and transaction investigation

Deployed migration 011 and `/api/v1/simulations/{id}` with a shareable `/simulations/{id}` page. A read-only repeatable-read transaction returns the run, bounded start/stop audit, observed/selected counts and a twenty-decision page. Run-scoped cursors prevent cross-run traversal. Indexed decision lookup and a unique audit action constraint support bounded output. Counts explicitly describe selection, not confirmed customer impact.

The PostgreSQL race test passes with 27 selected decisions, pages of 20 and 7 without duplicates, two audit entries with the recorded mitigation reason, stopped state, malformed-cursor rejection and cross-run cursor rejection. Gateway race tests cover the fixed detail destination and query filtering. Go vet, golangci-lint, frontend typecheck/lint and sequential production builds pass. Migration and service/gateway/frontend images are deployed.

All seventeen browser tests passed in one run. Extended simulation coverage follows the new run link, checks start/stop audit and actor identities, verifies a selected decision count, opens the actual failed transaction, exercises detail API failure/retry and checks 390px layout. Desktop and mobile investigation screenshots were visually inspected. The test-created run is stopped. Cross-request pages remain live; natural expiry is the recorded deadline, not an invented operator action.

## Database latency simulation

Deployed migration 012 and the database-latency scenario across simulation, payment, gateway and operator UI. Selected new reservations execute a parameterized PostgreSQL delay of 100–4500 ms before taking the account row lock. Persisted reservations replay without another delay; cancellation rolls back, and durable decisions preserve the selected configuration for an uncommitted retry. Stop/expiry prevent new selections without cancelling in-flight work.

Go workflow race tests passed in the dedicated disposable database, including real delay, cancellation/retry, reservation replay, bounds and stop/recovery. Go vet and golangci-lint passed with zero issues. Frontend typecheck/lint and sequential production image builds passed; migrations and updated services are deployed.

The public integration test created run `SIM-77ef220b9da1cf28d9d5d639`: purchase `TXN-c9e66d1d40ea85aab84c0abd` succeeded in 4258.429 ms, with a 4213224-microsecond PostgreSQL span in Jaeger. Exact purchase replay retained its identity and single decision. After stopping the run, a new purchase succeeded in 11.922 ms. Cleanup stopped the run in finally.

All eighteen browser tests passed together in 53.2 seconds, including reviewed latency configuration, a real delayed successful purchase, stop and run investigation. The deployed latency dialog was visually inspected at 1440px. This verifies bounded real database connection occupancy, not a database-wide outage or an automatic latency-specific incident rule; those remain separate work.

## Sustained latency detection

Added `ServiceLatencyHigh` and incident-worker ingestion, with explicit shared-runtime evidence and configured environment routing. Promtool tests pass for high latency with sufficient traffic, healthy latency, sparse observations and idle traffic. Incident-service race tests, Go vet and golangci-lint pass (zero lint issues). Runtime alert-to-incident verification remains pending; deterministic rule tests do not prove that deployed traffic has fired the rule.

Updated Prometheus and incident-service images built and deployed successfully. The live Prometheus rules API reports `ServiceLatencyHigh` loaded, health `ok`, and a thirty-second pending duration. Live induced latency-to-incident verification is the next check.

## Live latency alert-to-incident verification

`tests/integration/latency_alert.py` passed against Compose. Run `SIM-83a887c36043ca7d52b100ac` produced twenty-four successful staging purchases, each delayed by at least 4.1 seconds. Payment's ServiceLatencyHigh episode fired with activeAt `2026-09-27T09:12:04.679056893Z` and created incident `INC-b85806ca011b0a20724195a4`. Matching by service/title and exact detection timestamp found one incident; two further polling cycles retained the same incident and one creation audit entry. Severity is SEV-2, state Detected, measured impact remains null, and evidence explicitly states shared-runtime scope. Its development environment is configured routing, not attribution of staging traffic.

The test stopped its simulation in finally; a subsequent successful purchase completed in 15.485 ms. This establishes immediate transaction recovery, not immediate clearing of a five-minute histogram window. Gateway latency also fired as a separate service episode; cross-service grouping is not implemented. Additional promtool tests pass for the thirty-second pending interval and rule clearing after metric recovery. Python compilation passes.

## Kafka slowdown backend

Implemented migration 013, kafka-consumer-lag selection, current run activity in the decision response, payment pass-through and cancellable notification delay before delivery/offset commit. The delay occurs inside the consumer trace span. Compose configuration wires the optional local-only simulation client; images/migration are not deployed yet.

Notification race tests pass for selected delay, stopped/unselected/other-scenario bypass, invalid bounds, cancellation, lookup failure and malformed-event bypass to existing quarantine handling. Full workflow race suite passes in the dedicated disposable database, including a successful purchase under Kafka selection and preserved selection/current inactive status after stop. Go vet and golangci-lint pass. Remaining checks: operator UI contract, production builds, actual Kafka backlog, automatic lag incident and backlog drain after stop. These tests alone do not establish live Kafka behavior.

## Kafka operator controls and live backlog

Deployed migration 013 and updated simulation, payment, notification and frontend images. Reviewed UI accepts Kafka consumer lag with notification-delay labeling, validates bounded delay, explains shared consumer effects and bounded stop behavior, and displays the scenario in history/detail. Frontend typecheck/lint and all sequential production builds passed. Desktop dialog screenshot was visually inspected.

The first live request raced frontend startup and disconnected; a subsequent authoritative list confirmed no active run before retry. `tests/integration/kafka_lag.py` then passed: run `SIM-570f40d5477ad30ab9414743` generated forty successful purchases, observed actual lag of forty records, saw NotificationConsumerLag fire, and matched incident `INC-0533a2e45da6dd130ae78d8d` to that exact alert episode. The test stopped its run in finally and required lag to return to zero, which passed. This proves real asynchronous backlog/alert/drain behavior, not merely a delay unit test. Full browser regression is running.

All nineteen browser tests passed in one run (52.5 seconds), including Kafka scenario creation, prompt successful purchase, stop and notification-specific investigation labels. A read-only database join confirmed all forty decisions from the live Kafka run have durable notification deliveries (`40|40`), so zero lag was accompanied by actual delivery rather than only offset advancement.

## Alert evidence label correlation

Generated Prometheus queries now select all labels from the observed alert rather than only alertname. Race tests verify exact service scoping and correct escaping of quotes, backslashes and newlines through URL encoding, reject invalid/reserved label names, and require explicit current-state wording. Incident-service race tests, Go vet and golangci-lint pass with zero issues. Existing stored evidence is not rewritten.

The incident-service production image built successfully and was deployed. Its readiness endpoint returned ready after deployment. No new alert episode was induced for this link-only change; query construction is covered by focused tests.

## Database timeout implementation

Migration 014 and database-timeout decision support are implemented. Payment triggers a real PostgreSQL statement timeout inside a savepoint, restores transaction usability/settings and persists a DB_TIMEOUT failure with its outbox. The PostgreSQL workflow race test verifies elapsed delay, failed business outcome, unchanged Pulsa balance, one payment outbox event, stable retry and new successful purchase after stop. The complete workflow race suite passes; Go vet and golangci-lint pass with zero issues. Operator controls and investigation now distinguish statement timeout from delay. Production deployment, browser and trace/log/alert verification remain pending.

## Deployed database timeout

Sequential simulation/payment/migration/frontend builds passed and migration 014 deployed. Public integration run `SIM-38ef9f6c885a81965bd1d13e` produced five DB_TIMEOUT failures. Transaction `TXN-0642331a1616813a68ba2d51` has trace `1666c6a708bcbd23f06384c0f7c9910c` with a failed PostgreSQL SELECT span lasting 501661 microseconds. Structured gateway logs contain FAILED, DB_TIMEOUT, masked MSISDN and matching transaction/trace IDs. Exact replay after stop retained the failure; new transaction `TXN-b8cf639214a65b9eba2a0f67` succeeded. The test stopped its run in finally. Full browser suite is running.

Prometheus fired BusinessSuccessRateLow for staging with activeAt `2026-09-27T09:29:49.679056893Z`; automatic incident `INC-a6a5e9838012c5c296f6064e` carries that exact detection time and the newly scoped environment/severity evidence URL. The initial browser run passed nineteen cases; the timeout case exposed duplicated scenario copy in its footer. Corrected the footer to explain stop/retry behavior; typecheck and lint pass again. Frontend rebuild and focused simulation rerun follow.

The corrected frontend rebuilt and deployed successfully. All five simulation browser tests passed together (23.4 seconds), including the real 4.2-second timeout, DB_TIMEOUT result, reviewed stop and statement-timeout investigation label. Combined with nineteen passing cases from the full run, all twenty cases have passed; the final repeat was scoped to the changed simulation component.

## Splunk HEC transport groundwork

Added an isolated structured-event transport following the official HEC JSON event contract. Local receiver race tests verify authenticated event/index/sourcetype shape and retained correlation fields, invalid/oversized event rejection, malformed/nonzero/oversized response rejection, redirect refusal, cancellation and endpoint restrictions. Go vet passes; two initial style findings were corrected before rerunning lint. No application logs are exported yet and no actual Splunk indexing has been verified. Added example SPL for failed transactions, business success, timeout/trace correlation and version comparisons, explicitly marked unexecuted against a real index.

Final HEC race tests, Go vet and golangci-lint pass with zero issues.

## Bounded asynchronous log export

Added a 256-record, 64-KiB-per-record nonblocking queue with defensive copies, accepted/failed/dropped accounting and cancellable drain. Race tests verify overflow with a blocked receiver, buffer ownership, delivery failure, deadline cancellation, pending-record drop accounting and concurrent write/close without lost accounting. Optional environment-based exporter wiring now covers gateway and shared domain startup, preserves stdout and registers outcome/pending metrics. Focused package/runtime race tests, Go vet and golangci-lint pass with zero issues. Logger-to-receiver integration, production rebuild/deployment and actual Splunk indexing remain pending; no external receiver is configured.

## Configured log export and Compose overlay

A configured slog logger now has end-to-end local receiver coverage: it loads an integration-only token from a private test file, exports a structured DB_TIMEOUT event with service/transaction/masked MSISDN fields, drains and exposes accepted=1/pending=0 through Prometheus. Disabled/partial/ambiguous configuration cases are tested. Added bounded secret-file loading and an opt-in overlay covering all seven Go services with an outbound log network and mounted secret. Compose merged configuration validates using a fictional endpoint without enabling export or printing configuration secrets. Race tests, Go vet and lint pass; the final bounded-receiver test rerun also passes. Actual Splunk indexing and production image rollout remain unverified and disabled.

## Log export loss monitoring

Prometheus rule tests pass for failed exports, overflow drops, healthy accepted traffic, idle counters and absent metrics when export is disabled. Incident-service race tests verify SEV-3 shared-runtime scope and simulation-service support. Go vet and golangci-lint pass with zero issues. All Go service images and Prometheus are being built sequentially; external export remains disabled and actual Splunk indexing is still unverified.

`go test -race ./...` passes across the backend packages with default test configuration; tests requiring an explicit external database were not re-enabled in this command. Earlier dedicated PostgreSQL workflow verification remains separate evidence.

All seven Go service images and Prometheus built and deployed successfully with optional export disabled. The live rules API reports LogExportLoss healthy and inactive. Full browser regression is running against these rebuilt services.

All twenty browser tests passed in one run (58.4 seconds) against the rebuilt stack, including incident evidence, purchases, four simulation scenarios and real trace links. This verifies default-disabled exporter startup compatibility. Enabled export remains verified through local receiver tests; actual vendor indexing and an induced deployed log-loss incident are not claimed.

## Authenticated OTLP trace export

The exporter now preserves a configured OTLP base path and appends `/v1/traces`. Optional authorization loads from a bounded private file, requires HTTPS, rejects ambiguous header sources and refuses redirects. A trusted local TLS receiver test decoded actual OTLP protobuf, verified a payment-service span, exact `/api/v2/otlp/v1/traces` path and authorization header. Negative tests cover plaintext, embedded URL credentials, query-token URL, invalid/oversized file and redirect refusal. The optional seven-service Dynatrace Compose overlay validates with fictional configuration. Real Dynatrace ingestion and topology remain unverified; default Jaeger use is unchanged pending rebuild/deployment.

Authenticated OTLP test verification: a trusted local TLS receiver accepted an actual protobuf trace export, with `Authorization: Api-Token integration-only` and preserved `/api/v2/otlp/v1/traces` path. Tests reject plaintext authenticated export, URL credentials/query secrets, invalid or oversized authorization files, conflicting header sources and redirects. Go vet and golangci-lint report zero issues. The opt-in seven-service Dynatrace Compose overlay validates using a fictional endpoint. Real Dynatrace ingestion remains unverified; default-image rebuild is still running and has not been reported as deployed.

The sequential default-image build began and api-gateway image built. The subscriber-service `docker compose build`/buildx subprocess and original session remained live but produced no output for over five minutes; Docker/BuildKit and existing application containers were responsive. No deployed-image or post-deployment Jaeger claim is made for this increment. Resume observation of the same build handle before deciding recovery.

All seven default Go service images were subsequently verified by fresh Docker image creation timestamps and deployed with the Dynatrace overlay disabled. Each rebuilt service reported healthy. A new synthetic purchase exported a complete trace to local Jaeger: trace `53044e6a683e96ca66a59fff12de0641` contained 83 connected spans across six services, including HTTP, PostgreSQL and Kafka propagation. This verifies default Jaeger compatibility after the tracing change. Real Dynatrace ingestion remains unverified because no endpoint or scoped authorization file has been configured.

The full browser suite passed against the rebuilt stack: 20/20 tests in 1.6 minutes, covering incidents, purchases, service telemetry, all four simulation paths and real trace links. An initial restricted run failed to launch local Chrome (19 browser-launch failures, one non-browser test passed); the same suite passed when given Chrome process access. No application assertion failed in the restricted run.

## Deployment event tracking and correlation

Migration 015 created deployment records and append-only status events. The new deployment service and gateway were built and deployed locally; both became healthy. An authenticated in-container HTTP integration run (`ci-payment-c6a1beb677e24dd5`) persisted Pending → Running → Completed → Rolled Back with different completion and rollback actors. The exact event replay returned 200; a conflicting replay, a repeated timestamp and an invalid post-rollback transition returned 409. A gateway query for a ten-second window containing only the initial Pending event still returned the current Rolled Back record, and the detail endpoint returned all four immutable events. This proves reported-event persistence and time-window lookup, not a real CI or cluster rollout.

Frontend typecheck, lint and production build pass with `/deployments` and `/deployments/[id]` routes. The deployed staging page rendered the actual synthetic rollback and four-event history. The first full 21-case browser run passed twenty existing cases; the new case stopped on a Playwright strict-locator collision during a transient duplicate route tree. After scoping the locator, the focused deployment navigation/mobile test passed. A second focused browser test passed for the incident deployment-evidence panel, which explicitly labels time proximity as an investigation lead rather than root cause.

The deployment service was added to Prometheus scraping, optional Splunk/Dynatrace overlays and the incident alert service allowlist. Both overlay merges validate with fictional configuration, incident-service race tests and Go vet/lint pass, the rebuilt Prometheus image passes `promtool check config` with nine rules, and the live targets API reports `deployment-service` as `up`. Actual external vendor ingestion and CI-triggered delivery are still unverified.

After deploying the monitoring changes, the focused browser run passed deployment list/detail, incident deployment evidence and service-health recovery together (3/3 cases). The earlier full suite passed its twenty pre-existing cases; the new deployment case passed in the corrected focused runs.

A separate live Chrome check opened staging incident `INC-070b83a531f3aed1a48d9fc4` and found the persisted `ci-payment-c6a1beb677e24dd5` deployment in its same-service detection window, with a working link to the deployment record. The incident was synthetic and was advanced through all required audit states to Resolved at revision 7; its root-cause, mitigation and resolution text explicitly state that no runtime degradation or customer impact occurred.

## Synthetic bad deployment and rollback

Migration 016 and the updated simulation/payment/web images were applied to the local Compose stack. `tests/integration/bad_deployment.py` passed against the public API with run `SIM-c17de7100fda1eed48c7233d` and marker `sim-deploy-c17de7100fda1eed48c7233d`: the worker reported `Completed`, five new staging purchases persisted `BAD_DEPLOYMENT_PAYMENT_FAILURE`, a development purchase succeeded, explicit stop reported `Rolled Back`, and a new staging purchase succeeded. A failed transaction still returned its original outcome after stop. The test cleaned up the run in `finally`.

The actual Prometheus `BusinessSuccessRateLow` rule fired for staging and produced incident `INC-ed6d8c66668402c93309bff4`. Its detection-window query returned the payment marker, now Rolled Back, as a cross-service candidate. A payment log carried the run ID, deployment ID and release version; Jaeger trace `45e80817ab580c4ad969697fde6ad9b6` had 70 spans and its payment reserve span carried `deployment.id`, `service.version` and `simulation.run_id`. A duplicate JSON `version` field found during inspection was corrected to `release_version` and verified in a subsequent browser run.

The complete Go suite and Go vet pass. Frontend typecheck, lint and production image build pass. The focused deployment Chrome suite passed 3/3 tests, the new operator bad-deployment test passed, and the final full browser regression passed 24/24 tests in 1.3 minutes. This verifies a local behavioral release simulation and reported markers; it does not verify a real CI deployment, Kubernetes pod change or external Splunk/Dynatrace indexing.

## Bounded k6 business load

The official `grafana/k6:2.2.0` image ran `tests/load/purchases.js` on the local `telcopulse_frontend` network. A ten-second healthy smoke produced eleven terminal successful purchases with zero HTTP errors, unresolved outcomes or dropped iterations. The workload polls initially processing purchases and evaluates final domain status separately from HTTP status.

The final `tests/load/bad_deployment.py` run started staging simulation `SIM-58ab89a9597a35da56f8d533`, waited for deployment marker `sim-deploy-58ab89a9597a35da56f8d533`, and ran 15 seconds of degraded traffic. k6 recorded 16/16 `BAD_DEPLOYMENT_PAYMENT_FAILURE` outcomes with HTTP failures 0 and dropped iterations 0. Prometheus' staging failed-transaction counter increased from 27 to 43. The runner stopped the simulation, waited for its Rolled Back marker, and ran ten seconds of recovery traffic: 10/10 final `SUCCESS` outcomes, no HTTP failures, unresolved outcomes or dropped iterations. Both phases passed the new five-terminal-outcome threshold. This is local synthetic traffic and durable test data, not production capacity evidence.

## Internal operator authentication foundation

Migration 017 created separate auth users, hashed sessions and append-only authentication events. The auth-service image built and runs healthy on the internal Compose network, without a host port; Prometheus reports its target `up`. An ignored, mode-0600 bootstrap password was piped into the container's one-time command, which created `local-admin`; running the same command again left its credentials unchanged. The first Compose secret bind mount was rejected by Docker Desktop's file-sharing permissions, so the final bootstrap design does not rely on host mounts or a long-lived secret environment variable.

The live `tests/integration/auth_internal.py` check passed against auth-service and PostgreSQL. It verified generic 401s for unknown/wrong credentials, rejection without the internal bearer token, an append-only audit trigger, a 256-bit opaque token stored only as its SHA-256 hash, current-role reads and active-user denial, idempotent logout revocation, five-attempt fifteen-minute lockout, and successful login after restoring account state. The test did not print credentials or tokens. The complete Go test suite and `go vet ./...` pass. This does not establish gateway/UI login, authoritative RBAC on operator routes, SSO, or non-local security readiness.

## Operator incident permissions in protected mode

The gateway now forwards only its validated session role and actor to incident-service. The incident service checks Operator edits against the locked current revision: assignment, investigation details, nonterminal state changes and appended evidence are allowed; severity/title changes, evidence removal or rewriting, action-item changes, resolution/postmortem and reopening resolved incidents return 403. The permission check runs before the incident and audit writes in the same transaction. Go unit and PostgreSQL workflow tests cover allowed edits, forbidden severity changes, unchanged revisions/audit rows, and Commander severity authority. The complete Go suite and vet passed; frontend typecheck, lint and production image build passed.

The rebuilt incident-service, gateway and web images were deployed locally. With `AUTH_REQUIRED=true` and the test account temporarily assigned Operator, `tests/e2e/operator-role.spec.ts` passed in Chrome: it created an incident with existing evidence and a Commander action, acknowledged and assigned it through the restricted editor, appended a note, confirmed the audit actor was an authenticated operator, and received 403 for a direct severity change without a new version or audit entry. A first test-fixture request omitted the required Origin and received 403; a second used a full incident snapshot on the strict update endpoint and received 400. Both test requests were corrected before the passing run. The test account was restored to Administrator and the gateway to `AUTH_REQUIRED=false`; the status endpoint returned `{"required":false}`. The live role check verifies this local boundary, not SSO or production identity controls.

## Authenticated simulation audit

The gateway now forwards its validated actor and role for simulation start/stop commands. The simulation service rejects malformed or unauthorized paired headers and writes the validated actor to immutable start/stop audit rows in each transaction. Go tests verify spoofed browser headers never cross the gateway, Operator role cannot inject, malformed internal identities fail before the store, and distinct start/stop actors persist in the PostgreSQL workflow. The complete Go test suite passed.

The updated gateway and simulation-service images were rebuilt and deployed. In temporary protected mode, `tests/e2e/engineer-simulation-role.spec.ts` signed in with an Engineer role, sent forged actor/role headers, started and stopped a real development simulation, and confirmed that both audit rows contained the signed-in user's operator ID. The test stopped the run in `finally`; `local-admin` was restored to Administrator and `/api/v1/auth/status` returned `{"required":false}` after the gateway returned to demo mode. The existing demo-mode simulation start/inject/stop browser regression also passed against the rebuilt services. Go vet passed. A later service-capability increment prevents shared-token-only mutation calls; production-grade internal service identity is still outstanding.

## Local Administrator account management

Migration 018 added acting-administrator IDs and JSON details to the append-only auth event stream. The gateway now exposes protected local-account list/create/update routes; auth-service rechecks the current Administrator session independently for each operation. A database advisory lock serializes role/activation updates before enforcing the last-active-Administrator invariant. Deactivation revokes the subject's sessions transactionally. Responses contain profile, role, active state and creation time but no credential material.

The migration was applied to the running PostgreSQL database and the rebuilt gateway and auth-service became healthy. `tests/integration/auth_management.py` passed against the live internal API: duplicate username 409, Viewer list/create 403, role change reflected in an existing session, deactivation revoked it, the last Administrator could not be demoted, and the three management events carried the correct actor. The production web image built; TypeScript and lint passed. In temporary protected mode, `tests/e2e/admin-users.spec.ts` passed through the Settings UI for create, role change and deactivation, plus public API checks for Viewer denial and the last-Administrator guard. The complete Go suite and vet passed, and the existing internal auth lifecycle/lockout regression passed after changing its bootstrap assertion to allow additional managed accounts. The gateway was restored to `AUTH_REQUIRED=false`; the status endpoint returned `{"required":false}`. These are local accounts; password reset, SSO provisioning and production identity controls remain unverified and unimplemented.

## Service-specific operator mutation capabilities

`scripts/init-env.sh` now generates separate incident and simulation operator tokens in the ignored `.env`. Compose passes each only to the gateway and its corresponding domain service; a config assertion confirmed payment and notification receive neither. The gateway's RPC client supplies the matching capability on unsafe incident/simulation calls. Domain handlers require that capability in addition to the existing shared service bearer for incident POST/PUT and simulation start/stop. Shared-token reads and `/internal/simulation/decision` remain available. The incident alert worker writes through its store directly and does not need an operator HTTP capability.

The three changed images built and were deployed locally. `tests/integration/operator_mutation_boundary.py` confirmed all four mutation routes return 401 to a sibling holding only `SERVICE_TOKEN`, even with forged role headers, while incident and simulation GET routes return 200. The updated direct internal incident integration and public gateway incident integration each completed the full lifecycle with eleven audit entries. The existing demo-mode simulation browser regression passed start, injection and stop. Protected-mode Operator incident and Engineer simulation audit browser tests passed after the token split. Full Go tests, vet, shell syntax checks and Compose validation passed. `local-admin` was restored to active Administrator and `/api/v1/auth/status` returned `{"required":false}`. This closes the shared-token mutation bypass in the local stack; internal HTTP is still unencrypted, service identities are environment-backed bearer values, and rotation/mTLS remain unimplemented.

## Incident escalation coordination

The incident service, gateway and production web image built and were deployed locally. `tests/e2e/incident-escalation.spec.ts` passed in Chrome against the running stack: a handoff set owning team, owner, higher severity, level and time; the next audit revision retained exact before/after values and an `escalated` action; stale revision returned 409 and severity downgrade 422. All seven existing `tests/e2e/incidents.spec.ts` tests passed. The protected Operator browser test confirmed no Escalate control and a direct escalation POST returning 403 while its existing permitted edit and forbidden severity checks still passed. The account and gateway were restored to Administrator and demo mode, confirmed by `/api/v1/auth/status` returning `{"required":false}`.

Frontend typecheck/lint, production web build, the complete default Go test suite and `go vet` passed. The new database workflow test compiles but was skipped by the default Go run because `TEST_DATABASE_URL` was unset; the live browser/API test supplied the persisted-path verification. An initial sandboxed Chrome run aborted before page load, and a concurrent uncached Go run exhausted the host's limited free disk. Removing only generated Go and Docker build cache restored space; the subsequent browser and cached Go runs passed. This records an internal coordination handoff, not delivery or acknowledgement of an external on-call page.

## Structured corrective actions

The incident-service and web images were rebuilt and deployed. The new action model accepts P1/P2/P3 and Open/In Progress/Blocked/Completed, derives the legacy `done` field from explicit status, and upgrades done-only items to P2 with the matching Open/Completed status on a write. Focused Go tests cover legacy normalization, invalid values and Operator equality against old action data. Frontend typecheck/lint and production build pass.

All eight incident Chrome tests passed against the live stack. The action-edit test recorded P1/In Progress, then Completed, checked persisted `done=true`, and compared both audit snapshots; the existing old-payload incident and lifecycle cases still passed. The protected Operator Chrome test passed with a legacy done-only action and verified the unchanged field-level denial. The account was restored to Administrator and `/api/v1/auth/status` returned `{"required":false}`. The complete default Go suite and vet passed; the running incident-service, gateway and web report healthy. Database-only Go workflow tests remain separately gated by `TEST_DATABASE_URL`.

## Kubernetes evaluation packaging

`scripts/verify-helm.sh` passed Helm 3.22 strict lint and kubeconform v0.7.0 strict validation against Kubernetes 1.34 schemas. The rendered profiles contain 33 valid dev, 35 valid staging and 37 valid production-like resources; the optional bootstrap path contains 34 valid resources. The Namespace and empty Secret examples each validate. A rendered-manifest inspection confirmed ten Deployments with exact per-service Secret key scopes, probes, CPU/memory resources and non-root pod policy. Negative renders rejected disabled gateway authentication and a web origin that disagreed with the TLS Ingress host. The chart references an external Secret and external PostgreSQL/Redis/Kafka rather than embedding credentials or pretending to operate those data systems.

The web image's runtime cache was rebuilt for group-0 write access while its static assets remain read-only. A container running as UID 123456:GID 0 wrote to the cache, could not write to the static directory, and served HTTP successfully. The final image was deployed back to Compose, and the incident browser smoke passed; all application services remained running. `kubectl config current-context` reported no configured cluster, so this verification covers chart rendering, Kubernetes resource schemas and a container-level UID smoke only. It does **not** prove an actual Kubernetes/OpenShift rollout, ingress routing, SCC admission, HPA behavior, pod disruption handling or OOM/crash-loop exercises. The production-like values remain restricted to the application's current local mode and staging incident data scope.

## CI and artifact packaging

The new `.gitlab-ci.yml` parses as YAML with verify, quality and publish stages; a structural assertion confirmed the SonarQube gate precedes image publication. `sh -n` passed for the publisher and its contract test. `sh tests/ci/publish-images.sh` passed with a fake Docker CLI: an unprotected branch and malformed repository path caused zero Docker calls, while a protected default-branch simulation issued ten builds and pushes, included the web gateway build argument, and never placed the synthetic registry token in command arguments. The pinned Helm 3.22 container passed strict lint, rendered the dev profile and produced a non-empty chart package.

Against a disposable PostgreSQL 17 container, the gateway's `MIGRATE_ONLY=true` command applied migrations and the real `TestDistributedPurchase` integration passed. A separate race-enabled tracing integration passed. The attempted race-enabled workflow binary could not link because this workstation ran out of disk space (`ld: write() failed, errno=28`); that is **not** a passing race verification. Generated Go cache was removed and the disposable database/container cleaned up. The full GitLab pipeline, SonarQube quality gate, Docker-in-Docker runner, JFrog push and Jenkins promotion have not been exercised because those external systems are not configured here.

## Jenkins evaluation promotion

`bash -n` passed for the promotion script and its test. `bash tests/ci/promote-release.sh` passed with fake Helm, kubectl and Docker clients. It showed invalid SHA, missing pull Secret, missing image sets and an unknown rollback revision never reach release mutation; the deployment and rollback preflights did not mutate the release. A valid SHA checks ten manifests, upgrades with the expected tag, and runs an in-pod web/gateway smoke; a failed smoke triggers rollback to revision 4 and a second smoke; explicit revision 2 rollback and smoke-only mode take their respective paths. Helm 3.22 rendered the supplied `imagePullSecrets[0].name` override. The production-like action used `telcopulse-eval-production`, not a production namespace. No real cluster, registry, Jenkins interpreter or deployment was available, so this proves script control flow and Helm value rendering only. Actual image pulls, Kubernetes permissions, migrations, Helm hooks, availability checks and rollback recovery remain to be tested in an isolated cluster.

## Structured major-incident postmortems

Migration 019 was applied in a disposable PostgreSQL 17 container; `TestMajorPostmortemGenerationIsAtomic` passed against it. The test covers the transaction linking state, immutable report and audit entry, stale/duplicate conflicts, Operator denial, major-incident generic-transition rejection, and the database update/delete guard. `go test -p 2 ./incident-service ./api-gateway/internal/httpapi` passed, as did frontend typecheck and lint. The gateway, incident-service and web production images built and were deployed to the local Compose stack; the updated services became healthy.

The complete incident browser suite passed 8/8 against `http://localhost:3001`, including creation and resolution of a SEV-2 incident, generic Postmortem bypass rejection, Commander generation through the form, and checks of report fields, eight timeline entries, action-item snapshot and the `postmortem_generated` audit revision. A full-page desktop screenshot of the rendered report was inspected. This verifies the local flow only; protected-mode identity, externally sourced incident evidence and operational use of the report have not been exercised end to end.

## Incident runbooks

The runbook index names all eight failure classes required by the specification, with a separate guide for each. Each guide was compared with the current Prometheus rules, simulation scenarios, deployment contract and Kubernetes evaluation guide. A local Markdown link check found no missing file targets across the runbook set, README and incident documentation; all nine new runbook Markdown files have final newlines and no trailing whitespace, and `git diff --check` passed. This is documentation verification, not evidence that a real CrashLoopBackOff/OOM incident, external Splunk/Dynatrace/Datadog investigation or live Jenkins rollback has been performed.

## Rolling synthetic purchase SLO

The SLO unit test passed for no traffic, a healthy budget, an exact 99.9% threshold and a breach above 100% consumption. `TestPurchaseSLOUsesCompletedEnvironmentWindow` passed against a disposable PostgreSQL 17 database: development outcomes and a 31-day-old staging failure were excluded, while a newly committed staging purchase entered the 30-day window. The disposable container and data were removed. Frontend typecheck and lint passed; the production web image built. A focused Playwright test passed against a temporary local Next.js server with mocked session and overview APIs, confirming unknown/no-traffic and breached-budget rendering plus no horizontal overflow at 390 px. The temporary server and its generated cache were removed.

The first gateway image build completed before a small exact-threshold calculation fix. Its rebuild failed while copying source into BuildKit with `metadata_v2.db: input/output error`; host free space had fallen near 120 MiB, and Docker logs showed `no space left on device` and container-log/overlay I/O errors. Clearing npm's generated download cache freed about 4.5 GiB. Docker Desktop's supported force-stop/start recovered its daemon without removing volumes. The corrected gateway image then built successfully. The initial web image exited with an invalid package config after the storage failure, so it was rebuilt cleanly and deployed. Gateway, web and PostgreSQL reported healthy. `GET /api/v1/overview?environment=development` through the running web returned a persisted 30-day SLO with 111 completed synthetic outcomes (75 success, 36 failed) and a separate zero-count last-hour view, as expected. The focused Playwright suite passed **2/2** against `http://localhost:3001`: the rendered SLI matched the live API for development and staging, and mocked idle/breached states plus 390 px overflow were checked. The affected Go package suite (`shared/domain`, gateway store, HTTP API and workflow) passed, and all sixteen local Compose services remained running, with health checks green where defined. This confirms the local SLO flow, not customer-facing availability or production deployment.

## Operational response metrics

The incident and deployment services now calculate separate rolling 30-day operational aggregates, exposed through the gateway and shown on the ITOC overview. Incident counts, current SEV-1 counts, MTTA and MTTR use detected incidents in the selected environment; the response includes the acknowledged and resolved sample counts and returns null averages when there is no eligible sample. Escalations count immutable audit events within the window. Repeat candidates use the documented same-service, normalized-root-cause heuristic. Deployment failure rate uses reported terminal events within the window, with the current Failed or Rolled Back state as the numerator and no rate when no releases qualify. The UI keeps an unavailable source distinct from a zero count.

Both aggregation tests passed against a disposable PostgreSQL 17 database after applying the repository migrations. The affected Go packages passed `go test -p 2`, frontend typecheck and lint passed, and the incident, deployment, gateway and final web images built and were deployed to local Compose. The live development incident endpoint returned 168 detected records, 43 MTTA samples, 38 MTTR samples, two escalation events and 35 repeat candidates; the deployment endpoint returned zero eligible terminal releases and a null failure rate. These values reflect accumulated synthetic/local tests, not a production fleet. The final browser regression passed **15/15** tests across operations, incidents, deployments and SLO, including environment selection, independent source failure, no-sample display and mobile overflow. A standalone screenshot attempt could not launch a second Chrome process in the sandbox; the Playwright browser assertions completed successfully.

## Paired-window purchase SLO burn detection

The Prometheus rule now compares the synthetic purchase failure fraction with the 99.9% objective in paired 5m/1h and 30m/6h windows. The fast branch uses a 14.4× threshold and five-outcome short-window floor; the sustained branch uses 6× and thirty outcomes. Both branches share `BusinessSuccessRateLow`, preserving one incident identity for a continuous firing episode. The rebuilt image passed `promtool check config` with 13 rules and its rule fixtures passed for fast and sustained burn, below-threshold traffic, sparse and idle traffic, and recovery. The affected incident-service Go tests passed.

The updated Prometheus and incident-service images were deployed to local Compose. Before the controlled run, the live rule was inactive. `tests/integration/slo_burn.py` produced six intentional development declines; Prometheus recorded the outcomes, the fast burn alert fired, and incident `INC-5e706009f3b770c70c8c6b48` was ingested with the matching activation time and revised evidence text. The Grafana image was rebuilt and deployed; its dashboard API returned 14 panels including both new burn-rate views, and Prometheus returned a live development one-hour failure ratio. This proves the local synthetic detection-to-incident path. It does not prove external paging, continuous collection across gateway downtime, a contractual customer SLO or production behavior.

## Cross-incident corrective-action register

The incident service now returns a bounded, environment-scoped action register from current incident documents; the gateway exposes it and the console displays `/rca`. Its disposable PostgreSQL 17 integration test passed active/completed status, priority, owner and literal search filters, due-date ordering, legacy done-only normalization, JSON-null action arrays, pagination and rejection of a cursor reused with different filters. The disposable database was removed. The affected Go packages and vet, frontend typecheck/lint and the incident, gateway and production web image builds passed.

The first live request returned 503 because historical incidents store `action_items` as JSON `null`; the initial fixture covered missing/empty arrays only. The SQL now expands only JSON arrays and treats any other shape as empty, and the new fixture guards that case. After rebuilding and redeploying incident-service, the live API returned three rows with `more` and a next cursor. The focused Chrome suite passed **2/2**: it created a synthetic incident with open and completed actions, verified server filters and cursor forwarding, showed an overdue marker, opened the source incident, and checked both populated mobile and API-failure layouts for no page overflow. A full-page desktop screenshot was inspected. This is a current-document register; action IDs, stable cross-edit pagination, standalone ownership workflows and production data scale remain unverified.

The final combined browser regression passed **17/17** across the action register, incident lifecycle, deployment correlation, operational metrics and purchase SLO. The rebuilt local services remained available through the deployed web origin.

## RCA report library

The incident service now indexes only published immutable postmortem documents, with environment, current service/severity and literal narrative search filters and generation-time keyset pagination. Newly generated reports snapshot mitigation; older report documents were not rewritten and show an absent mitigation field. The gateway exposes the read-only API, and the `/rca/reports` console page links to the complete report and timeline while identifying current incident metadata separately from frozen findings. Migration 020 adds a report-generation index.

A fresh disposable PostgreSQL 17 database with repository migrations passed the report-index integration test for environment isolation, current versus frozen mitigation, legacy reports, service/severity and learning-text filters, descending order, cursor paging, filter-scoped cursor rejection and invalid inputs. The affected Go packages passed tests and vet; frontend typecheck/lint and production image builds passed. Rebuilt incident, gateway and web services were deployed in the local Compose stack. The final incident and action Playwright regression passed **11/11** against `http://localhost:3001`, including major-incident report generation, API/index verification, report search and source navigation, API failure recovery, and 390 px horizontal-overflow assertion. Desktop and mobile report screenshots were inspected. The data is local/synthetic; operational learning review, external paging, SSO and a production deployment have not been validated.

## Incident register discovery

The incident service now filters paged incidents by environment, state, severity, exact service, case-insensitive owner/title/ID text and optional detected-since time. Each bounded query keeps the existing update-time/ID keyset order, and the opaque cursor records all filters. The gateway forwards allowlisted query keys. The console offers server-backed search, state/severity selectors, service and owner fields, 24-hour/7-day/30-day detection presets and clear controls. Search fields debounce while filter changes reset pagination.

A fresh disposable PostgreSQL 17 database with repository migrations passed the integration test for environment isolation, combined filters, detected time, title/ID search, ordering, second-page keyset behavior, changed-filter cursor rejection and invalid inputs. The affected Go tests and vet, frontend typecheck/lint and production image builds passed. Rebuilt services were deployed to local Compose. The incident/action browser regression passed **12/12** against `http://localhost:3001`, including a persisted filtered incident and an invalid-time API request; the focused filter case passed again after the desktop search-width fix. Desktop and 390 px mobile captures were inspected, and the mobile document had no horizontal overflow. Search performance at production scale, saved views, column controls and alternative sort orders remain unverified or unimplemented.

## Incident-period Splunk log evidence

The optional incident-service reader calls the Splunk Search jobs endpoint with a fixed server-generated query for the persisted incident's environment, service and detection-time window. Only `2h`, `24h` and `72h` post-detection windows are accepted, each starting fifteen minutes before detection. The reader uses an operator token file and configured index, requires HTTPS outside loopback tests, and caps request duration, response size and returned rows. It projects selected fields, excludes raw subscriber identifiers, masks subscriber-number patterns in messages, and sends generic upstream failures to the browser. The incident page distinguishes unconfigured search, zero matching events, load failures and populated evidence.

Local protocol-receiver tests passed request scope, authentication, response parsing and rejection of invalid endpoint/index, HTTP errors, malformed or oversized data, and redirects. A disposable PostgreSQL 17 integration test passed persisted-incident scoping and window validation. The affected Go packages passed tests and vet; frontend typecheck and lint, production incident/gateway/web image builds, and the optional Compose overlay merge passed. The first live unconfigured request exposed a typed-nil interface panic; after correcting the constructor and redeploying, the default route returned its explicit unconfigured state. The final incident/action Playwright suite passed **13/13**, including mocked populated, empty and source-error states and a 390 px mobile overflow check. All fifteen long-running local Compose services were running, with health checks green where defined. No real Splunk endpoint, token or index was configured, so actual indexing and vendor search remain unverified.

## Incident trace and dependency evidence

Incident-service now searches Jaeger's stable v3 trace summaries for the persisted incident service and a 2/24/72-hour post-detection window, including fifteen minutes before detection. It fetches at most eight summary IDs and returns at most four traces after checking explicit environment context in their spans. The projection contains service and database nodes, observed parent edges, maximum span duration and error flags. Raw tags and arbitrary service responses are not sent to the browser. Query deadlines, response sizes, redirects and output counts are bounded. The incident page distinguishes unconfigured, empty, error and populated states and uses a validated public Jaeger base URL for trace links.

Local fake-Jaeger tests passed query scope, environment exclusion, edge/error projection and malformed/oversized/redirect failures; the gateway test verified that only `window` is forwarded and browser credentials do not cross to incident-service. The affected Go tests and vet, frontend typecheck/lint, Compose config and production image builds passed. The deployed incident/action Playwright regression passed **14/14**, including populated and empty trace states, source errors, viewer link and 390 px overflow. The first browser rerun used the default Compose port/origin after container recreation; restoring `WEB_PORT=3001` for both web and gateway resolved it.

The first live purchase-trace integration run failed because Kafka was OOM-killed during Docker builds; its transaction succeeded and was traced, but the notification consumer span could not arrive. After restarting Kafka and confirming health, `WEB_PORT=3001 python3 tests/integration/traces.py` passed with **83 connected spans across six services** and trace `98b90d4c165eb76eae4d673743b43603`. Incident `INC-e8708592246d5ff5e2e1062e`, created after that purchase, returned two real Jaeger-backed trace projections through `/api/v1/incidents/{id}/traces?window=2h`, including the verified trace. This validates the local Jaeger path, not Dynatrace ingestion, Kubernetes topology, or a production incident.

## Incident-period metric history

The incident service now queries Prometheus `/api/v1/query_range` for five fixed expressions based only on the persisted incident environment and affected service: synthetic business success fraction and transactions/minute by environment, plus service HTTP RPS, 5xx fraction and P95 latency across the shared runtime. The 2/24/72-hour windows start fifteen minutes before detection and end no later than the current time. Query step targets at most 120 evaluations, and each response is bounded by time, bytes, series and samples. Non-finite samples are omitted, not coerced to zero. The console presents timestamps, units and explicit environment/shared-runtime scope, with independent source, empty and per-series no-data states.

Local Prometheus-protocol tests passed fixed query shape, isolation from arbitrary browser PromQL, sparse/NaN handling, invalid endpoints and malformed/oversized/upstream failures. A fresh disposable PostgreSQL 17 database with repository migrations passed the persisted-incident endpoint test; the container was removed afterward. Gateway tests confirmed only the `window` parameter crosses the internal boundary. The affected Go packages and vet, frontend typecheck/lint, Compose config and production image builds passed. The deployed endpoint for incident `INC-e8708592246d5ff5e2e1062e` returned all five series; business success had 14 measured points, business throughput and HTTP RPS 53 each, and HTTP error/P95 14 each in the recent two-hour selection. The full incident/action Chromium suite passed **15/15**, including populated, no-data, source-error and 390 px mobile cases. Desktop/mobile screenshots were inspected. The values are local synthetic traffic; the query does not attribute shared-process HTTP traffic to one environment or infer customer impact.

## Incident saved views

Migration 021 stores named register filters in PostgreSQL by operator and environment. The gateway forwards only server-derived operator identity, and the incident service enforces ownership for listing, updating and deleting. Names are unique per operator/environment without case sensitivity; filters are validated and bounded, each operator is limited to twenty views, and time presets remain relative. In local mode the identity is the shared `local-operator`.

The full Go tests and vet, frontend typecheck/lint, and production image builds passed. Migration 021 was applied and the three affected services were deployed to local Compose; all fifteen services were running, with health checks green where defined. The incident/action Playwright regression passed **17/17**, including a saved view that survived reload, restored filters, accepted an update and was deleted. A direct live internal-service check created a view as one authenticated actor, confirmed it was invisible to another actor, rejected that actor's update and delete with 404, then deleted it as the owner. The optional Go PostgreSQL test was not run because the isolated temporary Go container could not download dependencies; the live ownership check exercised the persisted path instead. At this stage, column controls and alternate sort orders were still outstanding.

## Incident table sorting and columns

The incident list now supports server-side newest-update, newest-opened, oldest-opened and highest-severity orders. Keyset cursors include the sort mode and reject reuse after changing it. Migration 022 backfills an indexed `detected_at` column from all existing incident documents and keeps it synchronized on subsequent writes; it also adds the severity-order index. Migration 023 adds the new Actions column to saved views created before that column existed. TanStack Table controls visible columns on each bounded page, and saved views persist that configuration with the selected sort. The incident identity column cannot be hidden.

The full Go tests and vet, frontend typecheck/lint, and production images passed. Migrations 022–023 were applied locally. PostgreSQL reported **349** incident records, none missing the backfilled detected time, and both sort indexes present. The incident/action Chromium regression passed **18/18**, including saved sort/visibility restoration, opened-time pagination, severity ordering and rejection of a cursor reused with another sort; the focused deployed test passed **3/3** after adding an explicit View details action and mobile scroll hint. The 390 px register capture was inspected and showed no page overflow. All fifteen services remained running, with health checks green where defined. Server pagination remains bounded to 100 rows per request; production-scale latency has not been benchmarked. The migration for older saved views was applied, but no pre-existing local view remained to exercise that update path.

## Observed incident-period purchase impact

For incidents on the synthetic purchase path, the incident service reads completed purchase outcomes from a repeatable-read PostgreSQL snapshot in the persisted incident environment and a bounded detection window. It returns completed, successful and failed counts, distinct customer IDs with failed purchases, and twenty-row keyset pages of failure IDs, trace IDs and error codes. The query never projects raw subscriber numbers. Other services return `applicable: false`; the browser labels the cohort as observed correlation and leaves operator-assessed impact fields unchanged. Migration 024 indexes failed outcomes by environment and time.

The full Go tests and vet, frontend typecheck/lint and production images passed. Migration 024 and its index were verified in the live database. A browser test created a payment-service incident, ran a real insufficient-balance purchase, confirmed its failed transaction appeared in the API and table, and checked that the raw subscriber number was absent. A second test covered unsupported-service and 503 states; the 390 px impact capture was inspected with no document overflow. A saved-view update test briefly raced its server mutation during the first broad run (19/20); the UI now displays server-confirmed save/update status, and the final full incident/action suite passed **20/20**. A localhost-only temporary database bridge ran the gated PostgreSQL fixtures successfully: purchase-impact counts, environment/window isolation, twenty-row keyset pagination, changed-window cursor rejection, saved-view ownership and incident-list cursor/sort behavior. The bridge was removed. All sixteen Compose services were running, with health checks green where defined. Production-scale latency and real customer impact remain unverified.

Docker Desktop became unresponsive after an image-layer I/O error with roughly 120 MiB host space remaining. Generated Go caches and inactive temporary runtime installer caches were cleared, Docker Desktop was restarted with its supported forced-stop command, and unused Docker build cache was pruned. The rebuilt stack, migration and browser regression then passed; about 4.1 GiB host space remained after verification.

## Operations audit register

The read-only `/api/v1/audit` endpoint combines append-only incident, simulation and deployment events for one environment and exposes the actor, action, recorded time and source resource link. Incident changes include old/new values only for approved scalar fields; free-form notes and full snapshots stay out of this register. The `/audit` page supports source, actor, action and exact resource filters with bounded keyset pages. See [audit scope and limitations](audit.md).

The rollback-only PostgreSQL test exercised all three sources in recorded-time order, staging/development isolation, source/resource filters, cursor binding and non-disclosure of free-form incident text. A temporary localhost database bridge was removed after the test. The deployed unfiltered development API returned HTTP 200 with 25 events and a next page. Full Go tests and vet, frontend typecheck/lint and production build passed; rebuilt gateway and web containers are healthy. The full Chromium regression passed **44/44 active tests**, including real incident change, page filtering, cursor rejection, resource navigation and mobile error state; five protected-auth tests were skipped because this Compose run uses demo mode. Production-scale query latency, retention/export policy and coverage of authentication or transaction events are not yet verified.

## Workspace search

The Go gateway now projects up to five navigation hits each from persisted incidents, transactions, deployments and simulation runs, with a twenty-hit cap and development/staging scope. Exact IDs and trace IDs outrank prefix and descriptive matches. Only source ID, title, operational detail and timestamp leave the search endpoint; raw transaction results, incident impact and simulation reasons are excluded. The command palette debounces queries, offers direct source links, and retains the transaction explorer for broader searches.

A rollback-only PostgreSQL fixture confirmed all four sources, ranking, literal matching, environment isolation, trace-ID lookup and absence of private subscriber content. The temporary database bridge was removed. Full Go tests/vet, frontend typecheck/lint/build and rebuilt gateway/web images passed. The deployed API returned HTTP 200 with bounded real search results. The focused browser tests passed live incident navigation, Enter selection, environment switching, empty and error states, and 390 px page overflow. The complete Chromium regression passed **46/46 active tests**; five protected-auth cases were skipped in local demo mode. Production-scale substring search latency and exhaustive historical discovery remain unverified.

## Live failure-to-recovery episode

`tests/integration/live_incident_demo.py` drove a bounded development payment-decline simulation and saved source responses to `/private/tmp/telcopulse-live-incident-demo.json`. Healthy baseline `TXN-af7f4d9abf53691d9ba02ed5` succeeded. Simulation `SIM-b53425a7b81f01731780e169` caused six observed `SIMULATED_PAYMENT_DECLINED` purchases and was explicitly stopped. Prometheus fired `BusinessSuccessRateLow` at `2026-09-28T00:45:24.679056893Z`; alert ingestion created `INC-847d6430dbcb960829764314` for the same episode. Incident purchase impact included eight failed outcomes in an eleven-purchase cohort, including an injected failure; the Prometheus metric route returned 142 points. Local payment-service JSON logs included the same transaction ID, trace ID, environment, failed status and error code.

The initial broad latest-eight Jaeger lookup returned no incident traces despite direct retrieval of all six failed trace IDs. A purchase-operation query bounded around detection returned the failures. The incident-service adapter now uses that query for purchase-path incidents, falls back to its general search when empty, and treats the existing `transaction.outcome=FAILED` span attribute as a trace error even when HTTP is 200. Fake-Jaeger tests cover query scope and business-error projection; the incident-service Go tests and vet passed, and the rebuilt container was deployed. The live incident route returned four dependency graphs, including failed trace IDs with the gateway node marked as an error and payment-service on the path.

The incident passed Acknowledged, Investigating, escalation to Payments Platform, Identified, Mitigating and Monitoring. A first pair of sparse recovery batches aged out after a long host sleep, and the server correctly returned `insufficient_traffic`. The resumed exercise supplied steady healthy purchases until the server reported `meets_target`: **5/5** successes in the earlier five-minute window and **10/10** in the recent window at `2026-09-28T01:37:19Z`. The operator recorded recovery validation, moved to Resolved, generated a structured postmortem with an action item, and verified the final Postmortem state and nine audit actions. The controlled simulation remained stopped.

This local exercise verifies purchase, injected payment failure, Prometheus alerting, local metric/log/trace correlation, automatic incident creation and the operator lifecycle through postmortem. It does **not** prove the current 17-step definition-of-done demonstration: the script drove APIs rather than the entire Customer Simulator UI, no corresponding errors were indexed in Splunk, and no matching Dynatrace trace was inspected. Splunk reported `configured: false`. Enterprise identity, live Kubernetes/OpenShift deployment and production operation also remain unverified. The evidence file is an ephemeral local artifact; identifiers and measured results above provide a durable audit trail here.

After the trace change, the full deployed Chromium regression passed **46/46 active tests** with five protected-auth tests skipped in demo mode. Python compilation, incident-service Go tests/vet and `git diff --check` passed. All sixteen Compose services were running; services with health checks, including incident-service, were healthy.

The browser regression generated a separate development business-success alert at `2026-09-28T01:38:34Z`, after the completed exercise, and alert ingestion opened `INC-0f1fff001364e51fe4f15e52` in Detected. That later synthetic test episode is distinct from the Postmortem incident above; no failure simulation remained active. Its firing state reflects the alert rule's rolling failure window and should be allowed to clear or investigated as a separate episode before using the local overview for a clean-room demonstration.

## Datadog container evidence

The incident service now has an optional read-only Datadog v2 timeseries adapter for three fixed Kubernetes measurements: container restarts, memory working set and CPU usage. It sends only server-generated queries scoped to the persisted incident's `env` and `service` tags over a bounded detection window, then rechecks returned environment/service tags before exposing namespace, pod and timestamped values. It caps source bytes, series and points; an oversized, malformed, redirected or failed vendor response becomes a sanitized source error. The gateway forwards only the allowlisted `window` parameter, and the new incident panel distinguishes unconfigured, empty, measured and source-error states. [Configuration and source limitations](datadog.md) are documented alongside the optional Compose overlay.

The local fake-Datadog protocol tests passed request method/path/authentication, fixed query/time scope, cross-environment exclusion, projection, unsafe endpoint rejection and response failures. Incident-service Go tests/vet, gateway route tests/vet, frontend typecheck/lint and the three production image builds passed. The rebuilt incident, gateway and web services were deployed. The live route for `INC-847d6430dbcb960829764314` returned `configured: false`, `source: datadog`, `limited: false` and zero series, as expected without credentials. A focused browser test passed all four panel states, valid/invalid windows and 390 px overflow; desktop and mobile captures were inspected. All sixteen Compose services were running and all defined health checks were green; no failure simulation remained active.

The broad Chromium regression passed **46 active cases**, with five protected-auth cases skipped in local demo mode. One audit case lost its browser session during a nine-minute host clock jump; it passed immediately when rerun alone, so all **47 active cases passed across the broad and focused runs**, but there was no single uninterrupted full-suite pass. Earlier broad attempts similarly crossed long host sleep periods; a legitimate `PROCESSING` purchase response was found in a simulation test and its three related tests were changed to poll for terminal outcomes, then passed together. Real Datadog account access, Kubernetes Agent collection, pod restart/OOM evidence and cross-vendor incident correlation remain unverified.

## Kubernetes vendor configuration

The Helm evaluation chart now renders optional Splunk HEC, Splunk Search, authenticated OTLP/Dynatrace, Jaeger Search and Datadog metrics configuration. Vendor URLs and Secret key names appear in ConfigMaps/Deployment projections; token contents are absent. `scripts/verify-helm.sh` passed Helm lint, strict Kubernetes 1.34 schema validation for dev, staging, production-like, bootstrap and vendor-enabled profiles, a semantic check of default no-vendor isolation and per-pod Secret-key projection, and negative renders for HTTP HEC, credentialed Splunk URL, query-bearing Datadog URL, authenticated OTLP without HTTPS, and OTLP CA without HTTPS. The vendor example uses fictional hosts. `kubectl config current-context` reported no current context, so no cluster install or live vendor indexing was performed. This verifies manifest shape and configuration scope only, not the 17-step demo's Splunk/trace evidence requirements.

## Jenkins release-event reporting

`scripts/promote-release.sh` now checks the ten running image tags after a successful evaluation deploy, then reports one per-service `Completed` event to deployment-service. Successful explicit rollbacks also report ten records using the observed image SHA and a rollback revision label. Event IDs and timestamps derive from Helm's release revision and last-deployed time, so retrying the reporting step for the same release reuses identical payloads. The service bearer is read from the namespace runtime Secret and passed to curl through a private header file over a localhost-only port-forward; the token does not appear in command arguments.

The fake-client contract test passed deploy and rollback event payloads, preflight non-reporting, ten distinct service records, wrong-image rollback, ten `Failed` events after post-deploy and post-rollback smoke failure, reporting failure as a failed job without rolling back a healthy release, and no token in captured command output. Bash syntax checks passed. This is local control-flow evidence only: no Jenkins agent, live cluster, real port-forward, or database ingestion was exercised. Helm failures before smoke may still lack an event if deployment-service is unavailable. The expanded 31-step flagship demonstration still requires real vendor evidence and cluster failure/correlation checks.
