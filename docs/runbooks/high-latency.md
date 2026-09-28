# High latency

**Trigger and scope.** `ServiceLatencyHigh` uses the shared-runtime `telcopulse:http_p95_seconds:rate5m{service="payment-service"}` estimate: greater than one second for 30 seconds with at least twenty requests in five minutes. It does not establish which environment or customer cohort is affected. Histogram quantiles are bucket estimates, not individual request durations.

**Triage.** Compare payment and gateway P95 with request volume, error rate, business outcomes and the preceding window. Inspect several transaction durations and traces, especially payment's database span. The local `database-latency` scenario occupies a PostgreSQL connection with `pg_sleep` for 100–4500 ms; verify a matching simulation decision before attributing the delay to injection. A missing trace does not erase the measured request latency.

**Differentiate.** Check `database_connection_usage{service="payment-service"}` against `database_connections_max`, PostgreSQL query span duration, outbox age and any concurrent HTTP errors. A high HTTP P95 can come from connection waiting, a slow dependency or load; a successful slow purchase is not a business failure. If connection use approaches its limit, switch to [database connection exhaustion](database-connection-exhaustion.md).

**Mitigate.** Stop a verified active latency simulation through the console. For a real slow query or dependency, use a reviewed rollback, traffic reduction or query fix according to the environment's change control; preserve traces and query identifiers first. Do not increase pool size blindly because it can move saturation to PostgreSQL.

**Recover and close.** Compare fresh transactions and traces after mitigation; confirm P95 remains below the alert threshold over a new five-minute window with enough requests, business success remains stable, and connection use returns to baseline. The local [latency integration test](../../tests/integration/database_latency.py) demonstrates stop and a subsequent purchase below two seconds; it is not a production SLO. Record measured before/after values and any remaining uncertainty.
