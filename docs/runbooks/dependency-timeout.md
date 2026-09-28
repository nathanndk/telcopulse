# Dependency timeout

**Trigger and scope.** A selected `database-timeout` simulation produces a payment `DB_TIMEOUT` business failure by timing out a PostgreSQL statement. It is a controlled query, not a database-wide outage. External payment-provider timeout injection is not implemented; do not label those failures as verified without provider telemetry.

**Triage.** From a failed purchase, record the masked customer reference, transaction ID, trace ID, environment, error code and first failure time. Inspect the payment stage and the PostgreSQL child span in the local trace viewer. Search structured logs by transaction/trace ID; compare `DB_TIMEOUT` count, HTTP P95, database pool usage and business success in the same window. Check simulation decisions and recent deployment markers, then independently test the dependency's health.

**Differentiate.** `DB_TIMEOUT` can be the controlled statement timeout; `SIMULATED_PAYMENT_DECLINED` is a business decline; `BAD_DEPLOYMENT_PAYMENT_FAILURE` belongs to the synthetic bad release. A caller deadline, lost service network route and PostgreSQL statement timeout are not interchangeable. Trace errors may be missing if exporter delivery failed; retain the durable transaction outcome as primary evidence.

**Mitigate.** Stop a verified active timeout simulation and verify a new purchase succeeds. For a real dependency incident, route to the owning team, apply a reviewed rollback/failover or safe timeout/concurrency change, and preserve retry semantics. Do not replay failed payment with a different idempotency key without checking its persisted reservation; a duplicate charge risk matters even in a synthetic model.

**Recover and close.** Confirm new transactions succeed, timeouts and latency return to baseline, pending workflows finish, and the affected dependency stays healthy through Monitoring. Record the precise failing edge, duration and evidence; add a preventive action for timeout handling, capacity or dependency resilience.
