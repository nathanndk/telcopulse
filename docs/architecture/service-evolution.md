# Service architecture and messaging

## Phase 2: implemented service boundaries

- **Subscriber service** owns `subscriber.profiles`: active subscriber identity and masked lookup.
- **Package service** owns `package.catalog` and `package.activations`: catalog and idempotent entitlement activation.
- **Payment service** owns `payment.accounts`, `payment.reservations` and `payment.event_outbox`: available balances, reserve/confirm/release and insufficient-balance outcomes.
- **Notification service** owns `notification.deliveries`, its inbox/quarantine and `notification.event_outbox`: idempotent synthetic delivery receipts. No SMS/email is sent. Kafka delivery uses a durable inbox and manual consumer offsets.
- **Gateway** owns `purchase_workflows`, completed `transactions` and their audit records. It aggregates frontend APIs and orchestrates HTTP operations. It no longer directly queries domain-owned tables.

The services share one PostgreSQL instance in local Compose, with distinct schemas and code ownership. Database credentials are currently shared; least-privilege database identities remain part of security hardening. The migration job coordinates additive versioned migrations before service startup. A common Go module under `services/` avoids duplicate shared contracts while services compile and deploy independently.

## Consistency and recovery

The gateway validates catalog inputs, allocates a stable transaction/trace identity and persists the request before any payment operation. Workflow processing is serialized using a transaction-scoped advisory lock. Database transactions are not held open for domain mutations across HTTP: each domain service independently commits its own transaction.

1. Reserve payment (Pulsa funds become unavailable immediately).
2. Activate the package idempotently.
3. Confirm reservation; definitive activation failure releases it instead.
4. Atomically store the final transaction, workflow state, audit record and purchase-completed outbox event.
5. Publish to Kafka; the notification consumer records delivery independently.

Every domain operation carries the same transaction ID. The service stores its request and rejects conflicting reuse. A timeout or lost response is uncertain, not a business rejection: retry reads the already committed result. A definitive activation rejection is saved before release so recovery cannot accidentally change a failed purchase into an activation.

A gateway worker retries pending purchases every five seconds with bounded request deadlines and batches. It survives process restart because request snapshots and catalog context are in PostgreSQL. An unavailable dependency currently keeps a purchase pending until it recovers; operator cancellation/dead-letter handling remains future work. Pending work is not counted as completed business traffic. The API returns HTTP 202 with a `PROCESSING` transaction and Location header. The transaction explorer and detail view show pending work and refresh automatically. The same idempotency key resumes that purchase; a new key intentionally creates another one.

Internal calls require a generated service bearer credential, use deadlines and carry transaction/trace headers. These correlation headers are not yet OpenTelemetry spans. Services are only reachable on Compose's internal network. No proprietary credentials or actual customer data are used.

## Verified fault cases

Real HTTP handlers and PostgreSQL integration tests cover concurrent same-key requests, distinct purchases exceeding the available balance, a payment commit followed by a lost response, failed activation with released funds, and notification outage followed by background recovery. Domain operations are authenticated and tested for idempotent outcomes.

## Kafka delivery

The gateway uses a transactional outbox for committed state changes. An outbox publisher retries safely and marks delivery only after broker acknowledgement. Consumers use an inbox/deduplication key before mutating durable state. This provides at-least-once delivery with idempotent processing, not global exactly-once semantics. Invalid/conflicting messages are retained in `notification.dead_letters` before offsets advance. Broker-derived lag and operator replay remain subsequent work. Redis now caches only display catalogs and enforces a shared synthetic-purchase budget. See [event guarantees](events.md).
