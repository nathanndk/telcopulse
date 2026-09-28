#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
: "${DATABASE_URL:?Set DATABASE_URL to your local TelcoPulse database}"
./scripts/init-env.sh
if [[ -z "${SERVICE_TOKEN:-}" ]]; then
  SERVICE_TOKEN=$(sed -n 's/^SERVICE_TOKEN=//p' .env)
fi
if [[ -z "${INCIDENT_OPERATOR_TOKEN:-}" ]]; then
  INCIDENT_OPERATOR_TOKEN=$(sed -n 's/^INCIDENT_OPERATOR_TOKEN=//p' .env)
fi
if [[ -z "${SIMULATION_OPERATOR_TOKEN:-}" ]]; then
  SIMULATION_OPERATOR_TOKEN=$(sed -n 's/^SIMULATION_OPERATOR_TOKEN=//p' .env)
fi
if [[ -z "${NOTIFICATION_OPERATOR_TOKEN:-}" ]]; then
  NOTIFICATION_OPERATOR_TOKEN=$(sed -n 's/^NOTIFICATION_OPERATOR_TOKEN=//p' .env)
fi
export SERVICE_TOKEN INCIDENT_OPERATOR_TOKEN SIMULATION_OPERATOR_TOKEN NOTIFICATION_OPERATOR_TOKEN APP_MODE=local
export REDIS_URL="${REDIS_URL:-redis://127.0.0.1:16379/0}"
export KAFKA_BROKERS="${KAFKA_BROKERS:-127.0.0.1:19092}"
export SIMULATION_URL=http://127.0.0.1:18086
export DEPLOYMENT_URL=http://127.0.0.1:18087
mkdir -p services/bin
cd services
for name in subscriber-service package-service payment-service notification-service incident-service simulation-service deployment-service; do
  go build -o "bin/$name" "./$name/cmd"
done
go build -o bin/api-gateway ./api-gateway/cmd/api
MIGRATE_ONLY=true bin/api-gateway
pids=()
cleanup() { for pid in "${pids[@]}"; do kill "$pid" 2>/dev/null || true; done; wait || true; }
trap cleanup EXIT INT TERM
port=18081
for name in subscriber-service package-service payment-service notification-service incident-service simulation-service deployment-service; do
  LISTEN_ADDR="127.0.0.1:$port" "bin/$name" &
  pids+=("$!")
  port=$((port+1))
done
export SUBSCRIBER_URL=http://127.0.0.1:18081 PACKAGE_URL=http://127.0.0.1:18082 PAYMENT_URL=http://127.0.0.1:18083 INCIDENT_URL=http://127.0.0.1:18085
LISTEN_ADDR=127.0.0.1:8080 bin/api-gateway &
pids+=("$!")
# Bash 3 (macOS default) has no wait -n; stop all children if any service exits.
while :; do
  for pid in "${pids[@]}"; do
    if ! kill -0 "$pid" 2>/dev/null; then
      echo "A TelcoPulse service exited; stopping this local stack." >&2
      exit 1
    fi
  done
  sleep 1
done
