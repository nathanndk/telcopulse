#!/usr/bin/env bash
# In-place restore of the local Compose database from a backup set; the procedure
# of docs/runbooks/database-loss.md. It REPLACES the telcopulse database, so it
# requires --yes-replace-local-database. When PostgreSQL still answers, it first
# takes a safety backup so the restore itself can be undone.
# Usage: scripts/restore-database.sh telcopulse-YYYYmmddTHHMMSSZ.dump --yes-replace-local-database
set -euo pipefail
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"
name="${1:-}"
[[ "$name" =~ ^telcopulse-[0-9]{8}T[0-9]{6}Z\.dump$ ]] || { echo "usage: $0 telcopulse-YYYYmmddTHHMMSSZ.dump --yes-replace-local-database" >&2; exit 1; }
[[ "${2:-}" == --yes-replace-local-database ]] || { echo "refusing to replace the local database without --yes-replace-local-database" >&2; exit 1; }
base="${name%.dump}"
writers=(api-gateway auth-service subscriber-service package-service payment-service notification-service incident-service simulation-service deployment-service web postgres-backup)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
now_ms() { echo $(( $(date +%s%N) / 1000000 )); }
psql_admin() { docker compose exec -T postgres psql -X -q -v ON_ERROR_STOP=1 -U telcopulse -d postgres "$@"; }

for file in "$name" "$name.sha256" "$base.counts"; do
  docker compose cp "postgres-backup:/backups/$file" "$work/$file" >/dev/null 2>&1 || { echo "backup set incomplete: $file" >&2; exit 1; }
done
(cd "$work" && sha256sum --quiet -c "$name.sha256")
echo "verified $name"

if docker compose run --rm --no-deps -e BACKUP_ONCE=1 postgres-backup > "$work/safety.log" 2>&1; then
  echo "safety $(tail -1 "$work/safety.log")"
else
  echo "no safety backup possible (database already unreadable); continuing"
fi

outage_start=$(now_ms)
docker compose stop "${writers[@]}" >/dev/null 2>&1
echo "writers stopped"
psql_admin -c 'DROP DATABASE IF EXISTS telcopulse WITH (FORCE)' -c 'CREATE DATABASE telcopulse OWNER telcopulse'
docker compose cp "$work/$name" "postgres:/tmp/$name" >/dev/null 2>&1
restore_start=$(now_ms)
docker compose exec -T postgres pg_restore --exit-on-error --single-transaction -U telcopulse -d telcopulse "/tmp/$name"
restore_ms=$(( $(now_ms) - restore_start ))
docker compose exec -T postgres rm -f "/tmp/$name"
docker compose exec -T postgres psql -X -q -At -U telcopulse -d telcopulse > "$work/restored.counts" <<'SQL'
SELECT format('SELECT %L || E''\t'' || count(*) FROM %I.%I;', table_schema || '.' || table_name, table_schema, table_name)
FROM information_schema.tables
WHERE table_type = 'BASE TABLE' AND table_schema NOT IN ('pg_catalog', 'information_schema')
ORDER BY 1 \gexec
SQL
diff -u "$work/$base.counts" "$work/restored.counts" || { echo "restored row counts differ from the manifest; writers left stopped" >&2; exit 1; }
echo "restored $(wc -l < "$work/restored.counts") tables in ${restore_ms} ms; row counts match the manifest"

docker compose up -d --wait migrate "${writers[@]}" >/dev/null 2>&1
web_port="${WEB_PORT:-3000}"
until curl -fsS -o /dev/null "http://localhost:$web_port/api/v1/overview?environment=development"; do sleep 1; done
outage_ms=$(( $(now_ms) - outage_start ))
echo "services healthy; write outage ${outage_ms} ms"
jq -nc --arg backup "$name" --argjson restore_ms "$restore_ms" --argjson outage_ms "$outage_ms" '{backup: $backup, restore_ms: $restore_ms, write_outage_ms: $outage_ms}'
