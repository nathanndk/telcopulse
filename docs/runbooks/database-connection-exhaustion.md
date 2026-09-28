# Database connection exhaustion

**Trigger and scope.** Suspect this when payment or another Go service shows rising HTTP latency/timeouts while `database_connection_usage` approaches `database_connections_max`. Those are **application pgx pool** measurements; they do not by themselves report PostgreSQL's server-wide connection count or identify a leaking query. No dedicated exhaustion alert or injector is currently deployed.

**Triage.** Check the affected service's pool use, request rate and P95 over the same interval. Inspect traces for long PostgreSQL spans and payment logs for retryable errors. On the authorized database, inspect `pg_stat_activity` for session count, state and wait events and compare with configured limits; use a read-only query such as `SELECT state, wait_event_type, count(*) FROM pg_stat_activity GROUP BY state, wait_event_type;`. Do not copy SQL text or subscriber data into incident notes. Determine whether saturation is confined to one service or shared by all clients.

**Differentiate.** A `database-latency` simulation holds payment connections while selected operations sleep; high concurrency can fill its eight-connection local pool. A `database-timeout` simulation generates `DB_TIMEOUT` but does not prove pool exhaustion. A slow server, lock wait, leaked connection, too many replicas and an unreachable database need different responses. Check database health and deployment changes before assigning root cause.

**Mitigate.** Stop a verified active simulation. For real pressure, reduce the source of excess concurrency or roll back the offending change through approved controls. If a query or lock is identified, have the database owner choose a safe cancellation or fix; do not terminate sessions in bulk or increase every client's pool limit. Preserve a timestamped pool/server snapshot before changes.

**Recover and close.** Verify pool use falls below its cap, server sessions and waits stabilize, fresh purchases succeed without retry buildup, and workflow/outbox backlogs drain. Keep the incident in Monitoring long enough to cover fresh traffic. Capture the exact pressure source and a capacity or query-prevention action in the RCA.
