#!/usr/bin/env bash
# Checks both rendered Alertmanager variants and the routing/inhibition contract
# with amtool. Uses fake secret files; contacts no receiver.
set -euo pipefail
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
image=telcopulse-alertmanager-check
DOCKER_BUILDKIT=0 docker build -q -t "$image" "$repo_root/infrastructure/alertmanager" >/dev/null 2>&1

run() {
  local secrets="$1"; shift
  docker run --rm -i --entrypoint sh "$image" -c "
    set -eu
    mkdir -p /tmp/secrets
    for name in $secrets; do echo 123456789 > /tmp/secrets/\$name; done
    sed 's#/run/secrets/#/tmp/secrets/#g' /etc/alertmanager/entrypoint.sh > /tmp/render.sh
    sed -i 's#^exec /bin/alertmanager.*#exit 0#' /tmp/render.sh
    sh /tmp/render.sh >/dev/null
    sed -i 's#/run/secrets/#/tmp/secrets/#g' /tmp/alertmanager.yml
    $*"
}

route() { # labels expected-receivers
  run "alert_webhook_token telegram_bot_token telegram_chat_id heartbeat_url" \
    "amtool config routes test --config.file=/tmp/alertmanager.yml --verify.receivers=$2 $1 >/dev/null" \
    || { echo "route mismatch for $1 (expected $2)" >&2; exit 1; }
}

printf 'full variant: '
run "alert_webhook_token telegram_bot_token telegram_chat_id heartbeat_url" \
  'amtool check-config /tmp/alertmanager.yml >/dev/null && grep -c telegram_configs /tmp/alertmanager.yml'
printf 'local variant telegram blocks: '
run "alert_webhook_token" \
  'amtool check-config /tmp/alertmanager.yml >/dev/null && grep -c "telegram_configs\|url_file" /tmp/alertmanager.yml || true'

route 'alertname=ServiceUnavailable severity=SEV-1 service=payment-service' incident-ingest,oncall-page
route 'alertname=BusinessSuccessRateLow severity=SEV-2 environment=staging' incident-ingest,oncall-notify
route 'alertname=LogExportLoss severity=SEV-3 service=api-gateway' incident-ingest,ops-ticket
route 'alertname=Watchdog severity=none' heartbeat
route 'alertname=Unlabelled' incident-ingest,oncall-notify
echo 'routes: ok'

# Inhibition is evaluated by a running Alertmanager: post a cause and a symptom,
# then confirm only the symptom on the same service is suppressed.
container=$(docker run -d --rm -v /dev/null:/run/secrets/alert_webhook_token:ro "$image")
trap 'docker rm -f "$container" >/dev/null' EXIT
for _ in $(seq 30); do docker exec "$container" wget -qO- http://127.0.0.1:9093/-/ready >/dev/null 2>&1 && break; sleep 1; done
docker exec "$container" amtool --alertmanager.url=http://127.0.0.1:9093 alert add ServiceUnavailable severity=SEV-1 service=payment-service >/dev/null
docker exec "$container" amtool --alertmanager.url=http://127.0.0.1:9093 alert add ServiceLatencyHigh severity=SEV-2 service=payment-service >/dev/null
docker exec "$container" amtool --alertmanager.url=http://127.0.0.1:9093 alert add ServiceLatencyHigh severity=SEV-2 service=package-service >/dev/null
sleep 2
inhibited=$(docker exec "$container" wget -qO- 'http://127.0.0.1:9093/api/v2/alerts?active=false&inhibited=true&silenced=false&unprocessed=false' | grep -o '"inhibitedBy":\["[^]]*\]' | wc -l)
active=$(docker exec "$container" wget -qO- 'http://127.0.0.1:9093/api/v2/alerts?active=true&inhibited=false' | grep -o '"alertname":"[A-Za-z]*"' | sort | tr '\n' ' ')
[ "$inhibited" = 1 ] || { echo "expected one inhibited alert, got $inhibited" >&2; exit 1; }
case "$active" in *ServiceLatencyHigh*ServiceUnavailable*) ;; *) echo "unexpected active set: $active" >&2; exit 1 ;; esac
echo 'inhibition: ok'
