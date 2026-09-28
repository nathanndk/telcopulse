#!/usr/bin/env sh
set -eu
cd "$(dirname "$0")/.."
if [ ! -f .secrets/local-admin-password ]; then
  echo 'Run scripts/init-env.sh first.' >&2
  exit 1
fi
docker compose exec -T auth-service /usr/local/bin/service bootstrap < .secrets/local-admin-password
