# Database loss or corruption

**Alerts:** `PostgresDown` (SEV-1), `DatabaseBackupStale` (SEV-2), `DatabaseBackupFailing` / `DatabaseBackupMetricsMissing` (SEV-3), and indirectly `UserFacingProbeFailed`, `DependencyProbeFailed{dependency="postgres"}` and failing readiness of every Go service. **Objectives:** 1-hour RPO and 15-minute RTO locally; see [disaster recovery](../disaster-recovery.md).

## 1. Triage (first 5 minutes)

1. Acknowledge the incident and name an incident commander. Data loss is SEV-1 even if the console still loads.
2. Is the **server** down or is the **data** gone? `docker compose ps postgres` and `docker compose logs --tail=50 postgres`. Check the tcp probe (`probe_success{dependency="postgres"}`) and `pg_up`.
   - Server down, data intact (crash, OOM, full disk): restart it (`docker compose up -d postgres`), check `HostDiskWillFillIn24h` and free space, and stop here if the services recover. **Do not restore.**
   - Server up but the database or tables are missing, or data is wrong (bad migration, accidental `DELETE`): continue.
3. Freeze writes that could make things worse: pause synthetic traffic and active simulations. Note the last known good time.

## 2. Choose the backup

```sh
docker compose exec postgres-backup sh -c 'ls -1t /backups/*.dump | head'
docker compose exec postgres-backup cat /textfile/telcopulse_backup.prom
```

Pick the newest backup taken **before** the damage, not just the newest file. For a bad migration or deletion that happened at a known time, use the last set before it. Then prove the set restores before touching production data:

```sh
bash scripts/restore-drill.sh telcopulse-YYYYmmddTHHMMSSZ.dump
```

Continue only when the checksum, the row counts and the current migrations pass.

## 3. Restore

```sh
WEB_PORT=3000 bash scripts/restore-database.sh telcopulse-YYYYmmddTHHMMSSZ.dump --yes-replace-local-database
```

The script verifies the checksum and takes a safety backup if the database is still readable, so the restore can be undone. It then stops every writer (application services and the backup job), drops and recreates the database, restores in one transaction, compares row counts with the manifest, starts migrations and services, and waits for the console API. It prints the restore time and the write outage. If row counts differ it stops with writers **still stopped**; choose an older set.

In Kubernetes: scale the TelcoPulse Deployments to zero, restore from the PVC backup with `pg_restore --clean --single-transaction` against the managed database, run the migration Job by upgrading the release, then scale back. Restore provider-managed databases with the provider's point-in-time recovery instead.

## 4. Verify and communicate

- `PostgresDown` and the probe alerts resolve. A new synthetic purchase succeeds and appears in Transactions.
- `EventPublicationDelayed` clears as outboxes drain. Expect some **duplicate notifications** for purchases completed just before the backup (see [consequences](../disaster-recovery.md#consequences-of-restoring-to-a-backup)).
- Record in the incident: the backup used, the data-loss window (backup time to damage), the restore and outage durations, and transactions known to be lost.
- Watch the purchase success SLO: the outage consumes error budget, and the [error budget policy](../error-budget-policy.md) applies.

## 5. Afterwards

Write the postmortem. Decide whether the RPO was acceptable. If not, the corrective action is WAL archiving with point-in-time recovery, not more frequent dumps. Run `scripts/restore-drill.sh` against the next scheduled backup to confirm the job is healthy again.
