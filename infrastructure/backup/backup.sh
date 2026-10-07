#!/bin/sh
# Scheduled logical backup of the TelcoPulse database. Each run writes a custom-
# format pg_dump, its SHA-256 and a manifest of exact per-table row counts taken
# in the same snapshot, prunes old sets, and publishes node-exporter textfile
# metrics so Prometheus can alert on stale or failing backups.
set -eu
: "${PGHOST:=postgres}" "${PGDATABASE:=telcopulse}" "${PGUSER:=telcopulse}"
: "${BACKUP_DIR:=/backups}" "${TEXTFILE_DIR:=/textfile}"
: "${BACKUP_INTERVAL_SECONDS:=3600}" "${BACKUP_RETAIN:=48}"
export PGHOST PGDATABASE PGUSER
PGPASSWORD="$(cat /run/secrets/postgres_password)"
export PGPASSWORD
failures=0

metrics() { # status duration size
  now=$(date +%s)
  {
    echo "# HELP telcopulse_backup_last_attempt_timestamp_seconds Unix time of the latest backup attempt."
    echo "# TYPE telcopulse_backup_last_attempt_timestamp_seconds gauge"
    echo "telcopulse_backup_last_attempt_timestamp_seconds $now"
    echo "# HELP telcopulse_backup_failures_total Backup attempts that failed since the job started."
    echo "# TYPE telcopulse_backup_failures_total counter"
    echo "telcopulse_backup_failures_total $failures"
    if [ -n "${last_success:-}" ]; then
      echo "# HELP telcopulse_backup_last_success_timestamp_seconds Unix time of the latest verified backup."
      echo "# TYPE telcopulse_backup_last_success_timestamp_seconds gauge"
      echo "telcopulse_backup_last_success_timestamp_seconds $last_success"
      echo "# HELP telcopulse_backup_last_duration_seconds Duration of the latest successful backup."
      echo "# TYPE telcopulse_backup_last_duration_seconds gauge"
      echo "telcopulse_backup_last_duration_seconds $last_duration"
      echo "# HELP telcopulse_backup_last_size_bytes Size of the latest successful dump."
      echo "# TYPE telcopulse_backup_last_size_bytes gauge"
      echo "telcopulse_backup_last_size_bytes $last_size"
    fi
  } > "$TEXTFILE_DIR/telcopulse_backup.prom.tmp"
  mv "$TEXTFILE_DIR/telcopulse_backup.prom.tmp" "$TEXTFILE_DIR/telcopulse_backup.prom"
}

backup_once() {
  stamp=$(date -u +%Y%m%dT%H%M%SZ)
  base="$BACKUP_DIR/telcopulse-$stamp"
  started=$(date +%s)
  # Export one snapshot, then dump and count rows against that same snapshot.
  snapshot_dir=$(mktemp -d)
  mkfifo "$snapshot_dir/hold"
  psql -X -q -v ON_ERROR_STOP=1 -At > "$snapshot_dir/snapshot" < "$snapshot_dir/hold" &
  holder=$!
  exec 3> "$snapshot_dir/hold"
  echo "BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY; SELECT pg_export_snapshot();" >&3
  for _ in $(seq 50); do [ -s "$snapshot_dir/snapshot" ] && break; sleep 0.1; done
  snapshot=$(head -1 "$snapshot_dir/snapshot")
  [ -n "$snapshot" ] || { exec 3>&-; wait $holder || true; rm -rf "$snapshot_dir"; return 1; }
  status=0
  pg_dump --format=custom --compress=6 --snapshot="$snapshot" --file="$base.dump.partial" || status=$?
  if [ $status -eq 0 ]; then
    psql -X -q -v ON_ERROR_STOP=1 -At > "$base.counts.partial" <<SQL || status=$?
BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
SET TRANSACTION SNAPSHOT '$snapshot';
SELECT format('SELECT %L || E''\t'' || count(*) FROM %I.%I;', table_schema || '.' || table_name, table_schema, table_name)
FROM information_schema.tables
WHERE table_type = 'BASE TABLE' AND table_schema NOT IN ('pg_catalog', 'information_schema')
ORDER BY 1 \gexec
COMMIT;
SQL
  fi
  echo "COMMIT;" >&3
  exec 3>&-
  wait $holder || true
  rm -rf "$snapshot_dir"
  [ $status -eq 0 ] || { rm -f "$base".*.partial; return 1; }
  mv "$base.dump.partial" "$base.dump"
  mv "$base.counts.partial" "$base.counts"
  sha256sum "$base.dump" | sed "s#  $BACKUP_DIR/#  #" > "$base.dump.sha256"
  last_duration=$(( $(date +%s) - started ))
  last_size=$(stat -c %s "$base.dump")
  last_success=$(date +%s)
  echo "backup $base.dump: $last_size bytes, $(wc -l < "$base.counts") tables, ${last_duration}s"
  # Keep the newest BACKUP_RETAIN complete sets.
  ls -1t "$BACKUP_DIR"/telcopulse-*.dump | tail -n +$((BACKUP_RETAIN + 1)) | while read -r old; do
    rm -f "$old" "$old.sha256" "${old%.dump}.counts"
  done
}

mkdir -p "$BACKUP_DIR" "$TEXTFILE_DIR"
until pg_isready -q; do sleep 2; done
while :; do
  if backup_once; then ok=1; else ok=0; failures=$((failures + 1)); echo "backup failed" >&2; fi
  metrics
  if [ "${BACKUP_ONCE:-}" = "1" ]; then [ "$ok" = 1 ]; exit; fi
  sleep "$BACKUP_INTERVAL_SECONDS"
done
