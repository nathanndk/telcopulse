# Controlled failure simulation

The independent Go simulation-service owns run configuration, audit records and transaction decisions in schema `simulation` (migration 010). Both the service and payment integration require `APP_MODE=local`; internal endpoints use the existing service bearer token. Public gateway mutations retain origin protection. Human authentication/RBAC is still required before this is suitable for a shared environment.

Supported scenarios are `payment-decline`, `database-latency`, `database-timeout`, `kafka-consumer-lag`, and `bad-deployment` (details below). Shared controls are: an integer percentage from 1 through 100, scoped to development or staging, lasting 30–900 seconds. A reason is mandatory. Only one unexpired, unstopped run can exist per environment. Expiry is enforced when selecting a run using database time; it requires no worker or timer. Start records contain the scheduled expiry. Explicit stop has its own append-only audit entry; natural expiry is derived from that recorded deadline.

- `GET /api/v1/simulations?environment=staging`: newest twenty runs with active status.
- `POST /api/v1/simulations`: JSON `environment`, `scenario`, `percentage`, `duration_seconds`, `reason`; an Idempotency-Key is mandatory. Identical replay returns the original run without extending it. A changed payload or overlapping active run returns 409.
- `POST /api/v1/simulations/{id}/stop`: JSON `reason`; repeated stop does not append another audit event.

Payment calls the internal decision API before a new reservation. Simulation-service persists a decision keyed by transaction ID, with environment and selected run ID. A stable hash samples the configured percentage; small runs need not have exactly that observed percentage. Both injection and no-injection decisions persist, so retries never switch when a run starts, stops or expires. Stopping affects new decisions; it does not reverse already selected or committed failures.

A payment-decline injection returns an HTTP-successful domain outcome with status FAILED and error code `SIMULATED_PAYMENT_DECLINED`. Payment persists its normal reservation and Kafka outbox event without debiting the account. The purchase workflow records the business failure and its existing logs, metrics and trace. The simulation ID is logged with transaction/environment correlation at decision selection and retained in the decision table. HTTP availability can remain healthy while business success drops.

When configured simulation-service is unavailable, new payment reservations return a retryable error; the workflow remains pending rather than silently bypassing configured failures. Already persisted reservations, confirm and release remain independent of simulation availability. Native payment with no SIMULATION_URL retains ordinary behavior. Compose enables the integration.

Verification: `WEB_URL=http://localhost:3001 python3 tests/integration/simulations.py` starts a staging run, checks five failed transactions, development isolation, stop, new-purchase recovery and unchanged failed retry. It stops its own run in a finally block. Unit/integration fixtures must use a dedicated disposable database.

CPU/memory/Kubernetes failures, k6 and the complete detect-to-postmortem demonstration remain to implement. The implemented scenarios do not claim those capabilities.

## Operator controls

Customer Simulator now lists the latest twenty runs for the selected environment and provides reviewed scenario start and reason-required stop dialogs. The start dialog names the environment and describes scope, expiry and retry behavior. An uncertain start response retains its original command/key in component memory, even when the global environment changes or the dialog closes; exact retry confirms the original run without extending it. This recovery does not survive reload/navigation, and the UI explicitly asks the operator to keep the page open. Confirmed validation/conflict errors allow correction.

History failure disables new starts rather than assuming no active run. Active/stopped/expired labels come from the API. The server remains authoritative for overlapping runs and bounds. Stops retain the selected run ID and are safe to retry. Durable client recovery and other scenarios remain pending.

## Run investigation

Each run ID links to `/simulations/{id}`. `GET /api/v1/simulations/{id}?cursor=...` returns the run, its immutable start/stop audit, observed/selected counts and twenty transaction decisions. The response uses one read-only repeatable-read database transaction. Migration 011 indexes decisions by run/transaction ID and enforces one audit entry per action per run.

Decision pages use descending transaction ID with a run-scoped cursor. Concurrent new decisions can sort ahead of an existing cursor; refresh the first page to see them. Counts and audit are consistent within a response, not frozen across multiple page requests. Each decision links to transaction investigation, where the final business outcome and trace can be checked. A selected decision alone is not confirmed incident impact, proof of root cause, or a count of affected users. Natural expiry is represented by the scheduled deadline, not a fabricated operator audit action.

## Database latency

`database-latency` accepts `delay_ms` from 100 through 4500 in addition to the existing percentage, duration, environment and reason. Payment executes parameterized `SELECT pg_sleep($1)` in its transaction before acquiring the customer-account row lock. This is actual database connection occupancy and a measured PostgreSQL span; it does not simulate an optimized query becoming slow or a database-wide outage. A selected purchase can still succeed. Context cancellation aborts the operation and rolls back its payment transaction.

The gateway payment RPC has a six-second timeout within the existing ten-second request deadline; other RPC clients keep their four-second bound. This allows a 4.2-second database delay to complete in the normal local flow. Heavy concurrent injection can occupy payment's eight-connection pool and produce pending/retried workflows. Confirm/release and persisted reserve replays do not repeat the delay. An uncommitted reservation retry retains its selected scenario/delay even after stop, but a committed reservation replays immediately. Stop and expiry prevent selection for new transactions; neither cancels already executing database operations.

Migration 012 backfills existing runs as payment-decline with zero delay. Run configuration is immutable through the API and decision reads join that retained configuration. Decline creation with omitted delay remains compatible. The UI names both scenarios and displays delay in run investigation. Sampling is called selection rate because latency selection does not itself imply business failure.

`WEB_URL=http://localhost:3001 python3 tests/integration/database_latency.py` verifies a successful slow purchase, a real payment PostgreSQL SELECT span of at least 4.1 seconds, unchanged replay, one decision, explicit stop and a subsequent purchase below two seconds. The test stops its own run in finally. Database timeout and Kafka lag are also implemented below; resource-exhaustion and Kubernetes failure scenarios remain pending.

## Kafka consumer lag

The `kafka-consumer-lag` scenario uses the same 100–4500 ms delay bounds and durable transaction selection. Payment accepts this scenario without declining or delaying the reservation. Notification consumption consults that decision before delivery and offset commit. Selected records wait while their run remains active at lookup; a stopped or expired run preserves selection history but no longer starts delays. An already started wait is bounded and cancellable on shutdown. Retry attempts recheck activity. Simulation lookup failure retains the record for retry rather than acknowledging it.

The consumer processes one record at a time across its assigned partitions. This deliberately reduces throughput and can delay other environments behind a selected event in the shared consumer; selection is environment-scoped, but shared queue latency is not isolated. Poison records bypass simulation lookup and retain the existing durable dead-letter path. Stop should drain backlog without additional injected waits, subject to normal delivery capacity. Reviewed operator controls and run investigation are available. Live verification produced forty queued records, a firing lag alert and an automatic incident, then confirmed zero lag after stop.

## Database timeout

`database-timeout` interprets `delay_ms` as a 100–4500 ms PostgreSQL statement-timeout threshold. Selected new reservations establish a savepoint, set a transaction-local timeout and execute a query that exceeds it. Only the expected PostgreSQL statement-timeout cancellation is converted into `DB_TIMEOUT`. Rolling back the savepoint restores the previous setting and permits atomic persistence of the failed reservation and payment outbox event. No balance debit occurs. Unexpected database errors and caller cancellation remain infrastructure failures. Existing reservation/purchase retries preserve their outcome; stop permits new purchases to succeed. This is a controlled query timeout, not a claim of database-wide unavailability.

## Synthetic bad deployment

`bad-deployment` models a payment-release regression with version `1.4.0-sim-bad`. It does **not** change a container image or a Kubernetes workload. Starting a run atomically queues a `Completed` deployment marker in the simulation outbox. A background worker reports it to deployment-service with a stable event ID, retrying temporary failures. New payment failures are selected only after that marker is acknowledged. Selected reservations persist `FAILED` with `BAD_DEPLOYMENT_PAYMENT_FAILURE`, emit the normal payment event, and do not debit a Pulsa balance. The payment decision log and trace include the deployment ID and version.

Explicit stop queues a `Rolled Back` event; natural expiry also queues one at the recorded deadline. A rollback report cannot overtake the initial completion report. The run investigation page shows the marker ID and report state, and the deployment page shows both immutable events. Stop or expiry restores new-purchase behavior; existing transaction decisions and outcomes remain stable. The `BusinessSuccessRateLow` incident view searches the same environment across services because the alert metric is cross-service, and labels nearby deployments as candidates rather than causes.

`python3 tests/integration/bad_deployment.py` verifies event persistence, five real failed staging purchases, development isolation, rollback, new-purchase recovery and stable failure history. It stops its run in a `finally` block. Real CI or Kubernetes rollout and rollback remain separate work.
