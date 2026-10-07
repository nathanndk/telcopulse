# Disaster recovery

PostgreSQL is TelcoPulse's only durable source of truth: purchases, workflows, outboxes, incidents, simulations, deployments, accounts and audit history all live there. Kafka topics, Redis, Prometheus, Loki and Jaeger hold data that is rebuilt, re-published or allowed to expire. Disaster recovery therefore means recovering PostgreSQL within agreed objectives, and proving it regularly.

## Objectives

| Objective | Local Compose | Kubernetes evaluation (chart `backup.enabled`) | Production recommendation |
| --- | --- | --- | --- |
| **RPO**, maximum data loss | 1 hour (`BACKUP_INTERVAL_SECONDS=3600`) | 1 hour (`backup.schedule: "0 * * * *"`) | Minutes: continuous WAL archiving with point-in-time recovery (for example CloudNativePG or pgBackRest) to object storage in another failure domain |
| **RTO**, time to restored writes | 15 minutes, including detection and decision | 30 minutes | 30 minutes for a database of this size, re-measured as data grows |
| Backup retention | 48 hourly sets (2 days) | `backup.retain`, 48 by default | 7 daily and 4 weekly sets plus PITR window; encrypted, off-site |

Measured on this workstation (see [Verification](#verification)): detection 36.5 s, `pg_restore` 0.37 s, writes unavailable for 47.5 s, 98 s from loss to recovery. The database is small (about 1,000 rows), so restore time will grow with data; rerun the drill after load tests and before relying on the numbers.

## Backups

The `postgres-backup` Compose service ([`infrastructure/backup/backup.sh`](../infrastructure/backup/backup.sh)) runs hourly and on start. Each run:

1. Opens a read-only repeatable-read transaction and exports its snapshot. `pg_dump --format=custom` and an exact `count(*)` of every table both run against **that same snapshot**, so the `.counts` manifest describes exactly what the dump contains.
2. Writes `telcopulse-<UTC>.dump`, `.dump.sha256` and `.counts` to the `postgres_backups` volume, renaming partial files only after they succeed.
3. Keeps the newest `BACKUP_RETAIN` sets.
4. Publishes `telcopulse_backup_last_success_timestamp_seconds`, `_last_attempt_timestamp_seconds`, `_last_duration_seconds`, `_last_size_bytes` and `_failures_total` through node-exporter's textfile collector.

Alerts: `DatabaseBackupStale` (SEV-2, no verified backup for 2.5 hours, so the 1-hour RPO is at risk), `DatabaseBackupFailing` (SEV-3, a failed attempt in 2 hours) and `DatabaseBackupMetricsMissing` (SEV-3, the job has not reported for 2 hours).

**Limits.** The backup volume lives on the same Docker host as the database, which protects against logical loss (a dropped table, a bad migration, accidental deletion) but not against losing the host. Copy sets off the host for real disaster recovery. Logical dumps do not include Kafka offsets, Redis or telemetry stores. The password reaches the job as a Compose secret file, never as an environment variable.

In Kubernetes, `backup.enabled` creates a CronJob that runs `pg_dump` with the `DATABASE_URL` key of `existingSecret` into a PersistentVolumeClaim, plus a `DatabaseBackupStale` PrometheusRule based on kube-state-metrics' `kube_cronjob_status_last_successful_time` when `monitoring.prometheusRule.enabled` is set. It does not write row-count manifests.

## Drills

- **Restore drill**, non-destructive, at least weekly and after schema changes: `bash scripts/restore-drill.sh [backup]`. It copies the newest set (or the named one), verifies its SHA-256, restores into a throwaway PostgreSQL on an isolated network, compares every table's row count with the manifest, runs the **current** migrations against the restored copy to prove the backup can serve today's code, and prints per-step timings.
- **Database-loss game day**, destructive for local data, monthly or before a release that changes storage: `python3 tests/integration/database_loss.py`. It backs up, writes a purchase that will be lost, drops the database, waits for `PostgresDown`, runs [`scripts/restore-database.sh`](../scripts/restore-database.sh), and checks that the pre-backup purchase survives, the post-backup purchase is gone (the RPO in action), new purchases work and the alert resolves.

Record each run's numbers in [VERIFICATION.md](VERIFICATION.md). A failed drill is a SEV-3 incident with a corrective action.

## Consequences of restoring to a backup

Restoring rewinds every service schema to the backup time:

- Purchases, incidents, audit entries and accounts written after the backup are lost. Kafka still holds purchase events for lost transactions, and their offsets are already committed, so they are not redelivered.
- Outbox rows that were still pending at backup time are published again. Consumers deduplicate by event ID only against the restored inbox, so a customer can receive a **duplicate notification** for purchases that completed just before the backup.
- Synthetic balances return to their backup-time values.
- Prometheus, Loki and Jaeger keep their own history, so dashboards show the outage and the restore.

Announce the data-loss window in the incident and reconcile anything outside the synthetic environment before resuming traffic.

## Verification

- `tests/integration/database_loss.py` on 2026-10-07: backup `telcopulse-20261007T152031Z.dump`; `PostgresDown` fired 36.5 s after the drop; restore 366 ms; write outage 47.5 s; 97.9 s from loss to recovery; the pre-backup purchase was present, the post-backup purchase was gone, and a new purchase succeeded.
- `scripts/restore-drill.sh` on 2026-10-07: 34 tables and 1,005 rows matched the manifest; checksum and current migrations passed; restore 331 ms, 16.5 s in total (mostly starting the scratch server).
