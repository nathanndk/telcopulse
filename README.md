# TelcoPulse

An internal telecom ITOC platform for connecting customer transactions to operational evidence and incident response. Fictional architecture and synthetic subscribers only; not an official telecom product.

## Current platform

The Compose stack includes a Next.js console, a Go gateway and independent auth, subscriber, package, payment, notification, incident, simulation and deployment services. A durable purchase workflow coordinates payment reservations, entitlement activation, Kafka notifications, stage evidence and audit records. Idempotency prevents duplicate charges; MSISDNs are masked in API responses and logs.

Available pages include ITOC Overview, Services, Incidents, RCA & Actions, RCA reports, Transactions, Deployments, Audit Log, Customer Simulator and Settings, plus investigation details. The header Operations inbox shows environment-scoped active incidents and recent recorded changes with links to their source records. The UI uses measured business data, live Prometheus service telemetry and persisted incident/deployment evidence, with explicit empty and error states. HTTP health has shared-runtime scope across transaction environments; see [service health semantics](docs/service-health.md).

The overview also shows a [rolling 30-day synthetic purchase SLO and error budget](docs/slo.md) from durable transaction outcomes.
It also reports [30-day operational response metrics](docs/operational-metrics.md) from incident audit and deployment event records, with sample counts and explicit no-data states.
Failed synthetic purchases can be [replayed as linked new attempts](docs/transaction-replay.md) from the simulator or transaction investigation page.

**The full platform is still in development.** Incident operations, failure injection, local tracing, reported deployment tracking and optional protected local-account sessions with RBAC now exist. A [Helm chart](docs/kubernetes.md) packages the application for protected Kubernetes evaluation, the [GitLab CI definition](docs/ci-cd.md) covers verification and image publication, and a [Jenkins workflow](docs/deployment-automation.md) defines evaluation promotion and rollback. No live CI or cluster deployment has been verified. SSO, real Splunk/Dynatrace/Datadog integration, production deployment and remaining failure scenarios still require work. See the [implementation plan](docs/IMPLEMENTATION_PLAN.md), [authentication scope](docs/authentication.md), [current product brief](docs/PROJECT_GOAL.md), and [expanded active goal](docs/EXPANDED_PROJECT_GOAL.md).

Incident detail also has an optional [Datadog container evidence adapter](docs/datadog.md) for scoped Kubernetes restart, memory and CPU measurements. It is unconfigured in the default local stack and has not been verified against a real Datadog account or cluster.

## Start with Docker Compose

Prerequisites: Docker Desktop / Docker Engine with Compose.

```sh
./scripts/init-env.sh
docker compose up --build -d
```

Open [TelcoPulse](http://localhost:3000). Set `WEB_PORT=3001` before the Compose command if port 3000 is already occupied. The frontend, Kafka development listener (19092), and Redis (16379) are bound to loopback. PostgreSQL and the API use an internal network. `init-env.sh` generates an ignored local database password; it never overwrites an existing `.env`.

```sh
docker compose logs -f api-gateway
docker compose down
```

Named PostgreSQL and Kafka volumes preserve purchases and events across restarts. Do not add `--volumes` unless intentionally deleting local data.

## Local development

Requires Node 22+, Go 1.25+, PostgreSQL 15+, Apache Kafka and Redis. Start the local broker with `docker compose up -d kafka redis`; `KAFKA_BROKERS` defaults to `127.0.0.1:19092` and `REDIS_URL` to `redis://127.0.0.1:16379/0`. Use your own local database and connection credentials.

```sh
npm ci
export DATABASE_URL='postgres://USER:PASSWORD@127.0.0.1:5432/telcopulse?sslmode=disable'
./scripts/dev-services.sh
```

In another terminal, from the repository root:

```sh
npm run dev
```

Browse `http://localhost:3000`. API listens at `127.0.0.1:8080` by default. `API_URL` overrides the frontend's backend destination. `WEB_ORIGIN` overrides the API's accepted browser origin. Keep these aligned if changing the hostname or port.

Numbered SQL migrations run transactionally at gateway startup under a PostgreSQL advisory lock. Do not manually modify tables. The initial migration seeds three synthetic customers and three packages; it does not reset balances on restart.

## Try the purchase flow

1. Open Customer Simulator; choose Ayu Pratama, Internet Everyday and E-Wallet.
2. Start a synthetic transaction. Open **Investigate transaction** to inspect stages and correlation IDs.
3. Choose Citra Wijaya, the 25 GB package and Pulsa to produce `INSUFFICIENT_BALANCE`.
4. Inspect the failed transaction: no entitlement is created. The business success rate falls even though the API successfully records the failed outcome.
5. Explore Transactions by ID, trace, customer, status and environment.

External payment methods are synthetic approvals; Pulsa deducts from the seeded balance. No real payment provider is contacted. Development/staging filter transaction records; The local stack shares the synthetic customer catalog and balances between them.

## Verify

```sh
npm run lint
npm run typecheck
npm run build
cd services
go test -race ./...
go vet ./...
golangci-lint run
```

For integration tests, point `TEST_DATABASE_URL` at a **separate disposable test database**. Tests truncate purchase/workflow/service outcome tables and reset two synthetic balances. They skip explicitly when the variable is absent.

```sh
TEST_DATABASE_URL='postgres://USER:PASSWORD@127.0.0.1:5432/telcopulse_test?sslmode=disable' go test -race ./...
```

With the app running, run `npx playwright test` from the repository root. It uses installed Chrome by default; set `PW_CHANNEL=chromium` after `npx playwright install chromium` to use bundled Chromium. The browser tests create synthetic purchases in the configured application database.

The production build uses Next.js's supported webpack backend because this workstation's restricted process environment prevents Turbopack's build-time CSS worker from binding its IPC port.

## Architecture and safety boundary

Next.js contains presentation and a same-origin reverse proxy, not purchase business logic. Go services own validation, idempotency, durable orchestration and domain persistence. A purchase interrupted by an unavailable payment or package service is shown as Processing and recovered in the background. Notification or Kafka outages do not block completion: the transactional outbox retains the event until delivery can resume. PostgreSQL is the durable source of truth. The contract is validated by Zod on the client; backend validation remains authoritative.

`APP_MODE=local` is mandatory in this increment. Internal calls use the generated `SERVICE_TOKEN`; incident, simulation and notification replay operator mutations additionally require separate service-specific tokens held by the gateway and their target service. Optional protected mode adds local operator sessions and server-enforced roles. This is not a production authentication or SSO deployment. Do not expose this local development stack publicly; the remaining enterprise identity work is tracked in the plan.

See [architecture](docs/architecture.md), [API contract](docs/api.md), and [verification record](docs/VERIFICATION.md).

For incident response, start with the [runbook index](docs/runbooks/README.md). It covers high error rate, high latency, database connection exhaustion, Kafka lag, CrashLoopBackOff, OOMKilled, dependency timeout and bad deployment with evidence, mitigation and recovery checks.

## Verify asynchronous recovery

Include `TEST_KAFKA_BROKER=127.0.0.1:19092` with the Go integration command to test real publication and consumption using a dedicated test topic. Tests must use a disposable PostgreSQL database.

With Compose running, `WEB_PORT=3001 python3 tests/integration/kafka_recovery.py` verifies that purchases succeed while the notification consumer is stopped and while Kafka is paused, then checks eventual single delivery. It temporarily disrupts only this local Compose stack and restores both services in `finally` blocks. Use your actual frontend port.

See [event delivery guarantees](docs/architecture/events.md) for retry behavior and remaining operational work.

## Redis behavior

The display package catalog is cached for 30 seconds, with authoritative fallback on cache failure. Purchases always fetch the current package from its service. Redis holds no balances, payment outcomes or workflow state.

`PURCHASES_PER_MINUTE` defaults to 60 across all gateway replicas and environments. This is a shared local synthetic-traffic budget, not per-user authorization. Admission uses an atomic fixed window. At capacity the API returns 429 with `Retry-After`; if Redis is unavailable it returns 503 before starting a purchase. Retry with the same idempotency key. Read APIs and already-started workflow recovery remain available. Redis restarts reset ephemeral budgets.

Set `TEST_REDIS_URL=redis://127.0.0.1:16379/15` for real Redis integration tests. Tests use unique expiring keys and do not flush the database. Run `WEB_PORT=3001 python3 tests/integration/redis_recovery.py` to verify bounded cache fallback and purchase admission while Redis is paused, followed by same-key recovery.

Purchase outcomes, payment state changes and synthetic delivery receipts now publish to three versioned Kafka topics through separate service-owned outboxes. See the event-delivery documentation for atomicity, ordering and duplicate-handling guarantees. Run either the native application services or the full Compose application against a given broker; independent application databases require separate Kafka brokers/topics and consumer groups.

## Monitoring (implementation in progress)

Prometheus metrics and a provisioned Grafana operations dashboard are implemented. Go tests, containerized rule checks, live alert/lag recovery checks and all four browser tests pass. See [observability contracts and commands](docs/observability.md) and [verification status](docs/VERIFICATION.md). Loopback endpoints are Prometheus on 9090 and Grafana on 3002. Run `scripts/init-env.sh` to generate the separate Grafana administrator secret before building the new stack.

HTTP/PostgreSQL OpenTelemetry instrumentation and correlated HTTP logs are implemented and tested. Set `OTEL_EXPORTER_OTLP_ENDPOINT` to a reachable OTLP/HTTP base URL to enable export. Compose now exports to the local Jaeger viewer at http://localhost:16686 by default; durable Kafka producer/consumer trace propagation is also verified; see [tracing scope and verification](docs/observability.md).

The independent incident backend runs in Compose with eight lifecycle states, version-checked edits and atomic audit history, exposed through `/api/v1/incidents`. The incident register, investigation UI and audited lifecycle editor are available at `/incidents`; the register supports server-side state, severity, service, owner, title/ID and detection-time filters. Incident detail can show bounded indexed log evidence through an optional read-only Splunk Search connection, with an explicit unconfigured state in the default local stack. Manual creation, impact measurements, structured corrective actions, recorded team escalation and immutable structured postmortems for major incidents are supported. The `/rca` corrective-action register surfaces outstanding work across incidents with filters, due dates and source links; `/rca/reports` indexes published postmortems by cause and learning notes. A [read-only operations audit register](docs/audit.md) combines incident, simulation and deployment activity without exposing source snapshots. Automatic detection from local Prometheus rules is verified; durable alert delivery and external on-call paging remain pending. See [incident contracts and limitations](docs/incidents.md).

The local simulation-service supports bounded, environment-scoped payment-decline, database-latency, database-timeout, kafka-consumer-lag and synthetic bad-deployment runs through `/api/v1/simulations`. Decisions persist per transaction; explicit stop and automatic expiry affect new decisions, while retries retain their original outcomes. Kafka notification waits additionally check current run activity so stopping allows backlog drain. Shared consumer delays can affect other environments. See [simulation contracts and limitations](docs/simulations.md). Reviewed start/stop controls and recent run history are available in Customer Simulator. Each run opens an audit and transaction-decision investigation page. A [bounded k6 workload](docs/load-testing.md) measures final purchase outcomes and verifies the local bad-deployment failure/rollback/recovery path.

The local deployment service records authenticated, idempotent CI-style status events and exposes read-only deployment history through the gateway. The Deployments page and incident investigation show reported releases and rollbacks with bounded time-window correlation. See [deployment tracking](docs/deployments.md). These records are not proof of a real cluster rollout; CI/CD and Kubernetes deployment remain open.
