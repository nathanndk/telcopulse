#!/usr/bin/env bash
# Restore drill: proves that the latest (or a named) backup restores and matches
# its manifest, without touching the running database. It verifies the SHA-256,
# restores into a throwaway PostgreSQL on an isolated network, compares exact
# per-table row counts with the counts taken in the backup's snapshot, runs the
# current migrations against the restored copy, and reports each step's timing.
# Usage: scripts/restore-drill.sh [telcopulse-YYYYmmddTHHMMSSZ.dump]
set -euo pipefail
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"
work=$(mktemp -d)
suffix=$$
network="telcopulse-drill-$suffix"
database="telcopulse-drill-db-$suffix"
cleanup() {
  docker rm -f "$database" >/dev/null 2>&1 || true
  docker network rm "$network" >/dev/null 2>&1 || true
  rm -rf "$work"
}
trap cleanup EXIT
now_ms() { echo $(( $(date +%s%N) / 1000000 )); }
step() { printf '%-28s %6s ms\n' "$1" "$2"; }

drill_start=$(now_ms)
name="${1:-$(docker compose exec -T postgres-backup sh -c 'ls -1t /backups/telcopulse-*.dump | head -1' | xargs basename)}"
[[ "$name" =~ ^telcopulse-[0-9]{8}T[0-9]{6}Z\.dump$ ]] || { echo "invalid backup name: $name" >&2; exit 1; }
base="${name%.dump}"
t=$(now_ms)
for file in "$name" "$name.sha256" "$base.counts"; do
  docker compose cp "postgres-backup:/backups/$file" "$work/$file" >/dev/null 2>&1
done
step "copy backup set" $(( $(now_ms) - t ))

t=$(now_ms)
(cd "$work" && sha256sum --quiet -c "$name.sha256")
step "verify sha256" $(( $(now_ms) - t ))

t=$(now_ms)
docker network create --internal "$network" >/dev/null
docker run -d --name "$database" --network "$network" -e POSTGRES_USER=telcopulse -e POSTGRES_DB=telcopulse \
  -e POSTGRES_HOST_AUTH_METHOD=trust postgres:17-alpine >/dev/null
until docker exec "$database" pg_isready -q -U telcopulse -d telcopulse 2>/dev/null; do sleep 0.5; done
# The image's init script restarts the server once; wait for the final instance.
sleep 2
until docker exec "$database" pg_isready -q -U telcopulse -d telcopulse 2>/dev/null; do sleep 0.5; done
step "start scratch postgres" $(( $(now_ms) - t ))

t=$(now_ms)
docker cp "$work/$name" "$database:/tmp/$name" >/dev/null 2>&1
docker exec "$database" pg_restore --exit-on-error --single-transaction -U telcopulse -d telcopulse "/tmp/$name"
restore_ms=$(( $(now_ms) - t ))
step "pg_restore" "$restore_ms"

t=$(now_ms)
docker exec -i "$database" psql -X -q -At -U telcopulse -d telcopulse > "$work/restored.counts" <<'SQL'
SELECT format('SELECT %L || E''\t'' || count(*) FROM %I.%I;', table_schema || '.' || table_name, table_schema, table_name)
FROM information_schema.tables
WHERE table_type = 'BASE TABLE' AND table_schema NOT IN ('pg_catalog', 'information_schema')
ORDER BY 1 \gexec
SQL
if ! diff -u "$work/$base.counts" "$work/restored.counts"; then
  echo "row counts differ from the backup manifest" >&2
  exit 1
fi
tables=$(wc -l < "$work/restored.counts")
rows=$(awk -F'\t' '{ total += $2 } END { print total + 0 }' "$work/restored.counts")
step "compare row counts" $(( $(now_ms) - t ))

t=$(now_ms)
image=$(docker compose images migrate --format json | jq -r '.[0] | .Repository + ":" + .Tag')
docker run --rm --network "$network" -e APP_MODE=local -e MIGRATE_ONLY=true \
  -e DATABASE_URL="postgres://telcopulse@$database:5432/telcopulse?sslmode=disable" "$image" > "$work/migrate.log" 2>&1 ||
  { cat "$work/migrate.log" >&2; echo "current migrations failed on the restored database" >&2; exit 1; }
step "current migrations" $(( $(now_ms) - t ))

total=$(( $(now_ms) - drill_start ))
step "total" "$total"
jq -nc --arg backup "$name" --argjson tables "$tables" --argjson rows "$rows" --argjson restore_ms "$restore_ms" \
  --argjson total_ms "$total" --arg size "$(stat -c %s "$work/$name")" \
  '{backup: $backup, size_bytes: ($size | tonumber), tables: $tables, rows: $rows, checksum: "ok", counts_match: true, migrations: "ok", restore_ms: $restore_ms, total_ms: $total_ms}'
