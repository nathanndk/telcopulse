# Capacity model

How many synthetic purchases per second the platform sustains, what saturates first, and how replica counts, pools and autoscaling must be sized. Numbers come from `tests/load/capacity.py` on the local Compose stack. Rerun it after any change to the purchase path, the database or the host before trusting them.

## Method

`python3 tests/load/capacity.py` raises the gateway's purchase admission budget for the run and warms the gateway. It then runs [`tests/load/saturation.js`](../tests/load/saturation.js) with k6 at constant arrival rates (5, 10, 20, 40, … purchases/s, 45 s per step, 15 s rest). Each step polls every purchase to a final state, through the web console's proxy as a browser would. For each step it records offered and completed purchases, end-to-end p50/p95/p99, dropped iterations, technical failures, the three busiest Go processes' CPU, gateway and payment pool use, PostgreSQL connections, Kafka lag, outbox age, and host load and swap-in. A step **breaches** when p95 exceeds the 2 s latency objective, more than 1% of offered purchases do not complete, more than 1% of iterations are dropped, or technical failures exceed 0.1%. The run stops at the first breach.

The test waits until the host's 1-minute load average is below 8 before each step. A first attempt on 2026-10-07 started while the WSL2 VM was swapping after a reboot (load average 140) and failed at 5 purchases/s; those numbers measured the host, not TelcoPulse, and are discarded. The machine has 16 vCPUs and 7.9 GB RAM, shared with another Compose project, and runs PostgreSQL on a virtual disk.

## Results (2026-10-07, commit with workflow admission slots)

| Offered /s | Completed | p50 | p95 | p99 | Gateway CPU (cores) | Gateway pool peak | Payment pool peak | Outbox age peak | Verdict |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 5 | 225/225 | 47 ms | 288 ms | 734 ms | 0.07 | 1/20 | 2/8 | 0.9 s | healthy |
| 10 | 451/450 | 33 ms | 760 ms | 1.5 s | 0.09 | 12/20 | 3/8 | 1.0 s | healthy |
| 20 | 900/900 | 31 ms | 38 ms | 44 ms | 0.15 | 2/20 | 2/8 | 1.0 s | healthy |
| 40 | 1,744/1,800 | 37 ms | 3.2 s | 4.6 s | 0.29 | 17/20 | 7/8 | 12.6 s | **breach**: p95, 3% incomplete, 57 dropped |

**Sustainable throughput on this workstation: 20 purchases/s** with p95 well inside the objective; saturation begins between 20 and 40/s. The tail at 5 and 10/s comes from host swap-in during those steps (100–145 pages/s), not from queueing in the application.

## Bottlenecks, in the order they appeared

1. **Gateway pool self-deadlock (fixed).** Before this change the system failed at 20 purchases/s with CPU idle: 391 of 900 purchases stuck in Processing and p99 near the 10 s deadline. `pg_stat_activity` showed gateway transactions *idle in transaction* holding `pg_try_advisory_xact_lock`. Each running workflow holds its lock transaction and needs a **second** pooled connection to save progress. With ten concurrent workflows, all ten pool connections were lock holders waiting for each other. The gateway now admits at most half its pool as running workflows (`purchase_workflows_in_flight`). A request that finds no slot within 3 s is answered as Processing and finished by background recovery (`purchase_workflow_deferred_total`). The pool grew from 10 to 20. `TestBurstBeyondWorkflowSlotsCompletes` reproduces the deadlock: without the slots, 30 concurrent purchases time out.
2. **PostgreSQL commit throughput (current limit).** At 40/s the database committed about **640 transactions per second**, roughly 16 commits per purchase across the gateway workflow, payment, activation, three outboxes and their relays, and the notification inbox and receipt. `pg_test_fsync` on this disk measured 260–790 `fdatasync` per second between runs. Workflows slowed from ~30 ms to ~250 ms, all ten slots filled (Little's law: 40/s × 0.25 s = 10), and the outbox relay fell 12 s behind. CPU was still below 0.3 cores per service. **Adding application replicas does not raise this ceiling.** Faster storage, fewer commits per purchase (batching relay updates, merging workflow saves) or a different durability trade-off would.

## Sizing rules

- **CPU per purchase/s:** gateway ≈ 7.5 millicores, payment ≈ 7, notification ≈ 4. A 100m request therefore reaches the 70% HPA target at about 9 purchases/s per pod. `values-production.yaml` requests 200m for the gateway, so a pod scales out near 19 purchases/s, which matches the per-pod throughput measured here. CPU-based autoscaling protects against CPU-bound spikes only; it cannot help when the database is the limit.
- **Workflow concurrency per gateway pod:** `DATABASE_MAX_CONNS / 2` (Compose 20 → 10; chart default 12 → 6). At 30 ms per workflow, 6 slots allow about 200 purchases/s per pod in theory, far above the database ceiling.
- **PostgreSQL connection budget:** `gateway replicas × DATABASE_MAX_CONNS + 8 × other Go pods + reserve ≤ max_connections`. Autoscaling multiplies the gateway term by `maxReplicas`, and a canary adds one pod. The chart's [`validate-capacity.yaml`](../infrastructure/helm/telcopulse/templates/validate-capacity.yaml) refuses to render a release that exceeds `capacity.databaseMaxConnections`. Without this check, the production-like profile would have needed 5 gateways × 10 (the old pool) + 64 = 114 connections before any reserve, or 174 with the new 20-connection pool, against PostgreSQL's 100. It now uses a 5-connection gateway pool (2 slots per pod), and staging uses 12. Beyond that, put PgBouncer in front of PostgreSQL rather than raising `max_connections`.
- **Headroom target:** run at most 50% of measured sustainable throughput (10 purchases/s here). Rerun this test when the deployment's storage changes; cloud SSD storage will move bottleneck 2 substantially.

## Alerts tied to these limits

`PostgresConnectionsHigh` (more than 80% of `max_connections`) and the database pool panel cover the connection budget. `PurchaseLatencyBudgetBurn` and `EventPublicationDelayed` are the first symptoms of commit saturation. `purchase_workflows_in_flight` at half the pool for minutes means the gateway is queueing, so check PostgreSQL commit latency before adding replicas.
