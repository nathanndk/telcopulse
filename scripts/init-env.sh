#!/usr/bin/env sh
set -eu
cd "$(dirname "$0")/.."
umask 077
if [ ! -f .env ]; then
  printf 'POSTGRES_PASSWORD=%s\n' "$(openssl rand -hex 24)" > .env
fi
if ! grep -q '^SERVICE_TOKEN=' .env; then
  printf 'SERVICE_TOKEN=%s\n' "$(openssl rand -hex 32)" >> .env
fi
if ! grep -q '^INCIDENT_OPERATOR_TOKEN=' .env; then
  printf 'INCIDENT_OPERATOR_TOKEN=%s\n' "$(openssl rand -hex 32)" >> .env
fi
if ! grep -q '^SIMULATION_OPERATOR_TOKEN=' .env; then
  printf 'SIMULATION_OPERATOR_TOKEN=%s\n' "$(openssl rand -hex 32)" >> .env
fi
if ! grep -q '^NOTIFICATION_OPERATOR_TOKEN=' .env; then
  printf 'NOTIFICATION_OPERATOR_TOKEN=%s\n' "$(openssl rand -hex 32)" >> .env
fi
if ! grep -q '^GRAFANA_ADMIN_PASSWORD=' .env; then
  printf 'GRAFANA_ADMIN_PASSWORD=%s\n' "$(openssl rand -hex 32)" >> .env
fi
mkdir -p .secrets
chmod 0700 .secrets
if [ ! -f .secrets/local-admin-password ]; then
  openssl rand -hex 24 > .secrets/local-admin-password
fi
echo 'Local database, service, and bootstrap operator credentials are configured in ignored files.'
