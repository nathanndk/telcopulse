# Local purchase load testing

`tests/load/purchases.js` drives real synthetic purchases through the Compose web/API path with k6. It uses a constant arrival rate so the offered traffic does not slow down when purchases become slow. Each iteration starts with a new idempotency key and waits for a final transaction state if the gateway initially returns `PROCESSING`. Its business rate counts `SUCCESS` or a configured failure code from the final domain outcome, independently of HTTP `2xx`. Unresolved outcomes, HTTP errors, dropped iterations, and fewer than five terminal outcomes fail the run.

The script accepts only the local Compose web endpoint and development/staging environments. Defaults are one purchase per second, 15 seconds, eight preallocated VUs, and a healthy-business threshold of 99%. Configuration is capped at 20 iterations/s, 300 seconds, and 50 VUs. The gateway's purchase-admission budget may need deliberate adjustment for higher rates; a `429` fails the load check. E-Wallet and seeded customer/package IDs avoid balance depletion. Every iteration still creates durable synthetic transaction, payment, activation, Kafka and notification records.

Run a short healthy smoke test after `WEB_PORT=3001 docker compose up -d`:

```sh
docker run --rm --network telcopulse_frontend -i \
  -e EXPECT=healthy -e RATE=1 -e DURATION_SECONDS=10 \
  grafana/k6:2.2.0 run - < tests/load/purchases.js
```

For a controlled synthetic bad-release degradation and rollback, run:

```sh
WEB_URL=http://localhost:3001 python3 tests/load/bad_deployment.py
```

The runner starts a staging `bad-deployment` simulation, waits for its `Completed` marker, executes a 15-second k6 run that requires `BAD_DEPLOYMENT_PAYMENT_FAILURE`, verifies the Prometheus failed-transaction counter increases, and always stops the simulation. It then waits for the `Rolled Back` marker and runs a 10-second healthy recovery load. Each phase requires at least five final outcomes. A failed threshold or unrecovered service returns a nonzero exit status. It uses the local `telcopulse_frontend` Docker network and the official `grafana/k6:2.2.0` image; it does not target production or perform a Kubernetes rollout.

For a manual failure experiment, use `EXPECT=degraded` and `FAILURE_CODE=BAD_DEPLOYMENT_PAYMENT_FAILURE` while the reviewed simulation is active. `MIN_TARGET_RATE` may be set from `0` to `1` (exclusive of zero) to match the configured selection percentage. Always stop an injected run after the experiment. These load tests provide measured business outcomes; inspect Prometheus/Grafana and the incident record separately for alert timing and investigation evidence.
