#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
chart_root="$repo_root/infrastructure/helm"
render_dir="$(mktemp -d)"
trap 'rm -rf "$render_dir"' EXIT

helm_image=alpine/helm:3.22.0
schema_image=ghcr.io/yannh/kubeconform:v0.7.0
# Prometheus Operator and Argo CRD schemas for the optional monitoring/delivery resources.
crd_schemas='https://raw.githubusercontent.com/datreeio/CRDs-catalog/main/{{.Group}}/{{.ResourceKind}}_{{.ResourceAPIVersion}}.json'

"$repo_root/scripts/sync-helm-rules.sh" --check

tar -C "$chart_root" -cf - telcopulse | docker run --rm -i --entrypoint sh "$helm_image" -c \
  'mkdir /chart && tar -xf - -C /chart && helm lint --strict /chart/telcopulse'

for profile in dev staging production; do
  tar -C "$chart_root" -cf - telcopulse | docker run --rm -i --entrypoint sh "$helm_image" -c \
    "mkdir /chart && tar -xf - -C /chart && helm template telcopulse /chart/telcopulse --namespace telcopulse-$profile -f /chart/telcopulse/values-$profile.yaml" \
    > "$render_dir/$profile.yaml"
  printf '%s: ' "$profile"
  docker run --rm -i "$schema_image" -summary -strict -kubernetes-version 1.34.0 \
    < "$render_dir/$profile.yaml"
done

tar -C "$chart_root" -cf - telcopulse | docker run --rm -i --entrypoint sh "$helm_image" -c \
  'mkdir /chart && tar -xf - -C /chart && helm template telcopulse /chart/telcopulse --namespace telcopulse-dev -f /chart/telcopulse/values-dev.yaml -f /chart/telcopulse/values-vendors.example.yaml' \
  > "$render_dir/vendors.yaml"
printf 'vendors: '
docker run --rm -i "$schema_image" -summary -strict -kubernetes-version 1.34.0 \
  < "$render_dir/vendors.yaml"
if command -v ruby >/dev/null; then
  ruby "$repo_root/scripts/check-helm-vendors.rb" "$render_dir/dev.yaml" "$render_dir/vendors.yaml"
else
  docker run --rm -v "$repo_root/scripts:/scripts:ro" -v "$render_dir:/render:ro" ruby:3.4-alpine \
    ruby /scripts/check-helm-vendors.rb /render/dev.yaml /render/vendors.yaml
fi

for invalid in \
  'observability.splunk.hecURL=http://splunk.example.invalid/services/collector/event' \
  'observability.splunk.searchURL=https://user@host.example.invalid/services/search/v2/jobs' \
  'observability.datadog.metricsURL=https://host.example.invalid/api/v2/query/timeseries?token=bad' \
  'observability.otlp.authorizationKey=OTLP_AUTHORIZATION' \
  'observability.otlp.certificateKey=OTLP_CA_CERTIFICATE'; do
  if tar -C "$chart_root" -cf - telcopulse | docker run --rm -i --entrypoint sh "$helm_image" -c \
    "mkdir /chart && tar -xf - -C /chart && helm template telcopulse /chart/telcopulse --set '$invalid'" \
    > /dev/null 2> "$render_dir/invalid.log"; then
    printf 'expected Helm to reject %s\n' "$invalid" >&2
    exit 1
  fi
done

tar -C "$chart_root" -cf - telcopulse | docker run --rm -i --entrypoint sh "$helm_image" -c \
  'mkdir /chart && tar -xf - -C /chart && helm template telcopulse /chart/telcopulse --namespace telcopulse-dev -f /chart/telcopulse/values-dev.yaml --set bootstrap.enabled=true' \
  > "$render_dir/bootstrap.yaml"
printf 'bootstrap: '
docker run --rm -i "$schema_image" -summary -strict -kubernetes-version 1.34.0 \
  < "$render_dir/bootstrap.yaml"

tar -C "$chart_root" -cf - telcopulse | docker run --rm -i --entrypoint sh "$helm_image" -c \
  'mkdir /chart && tar -xf - -C /chart && helm template telcopulse /chart/telcopulse --namespace telcopulse-dev -f /chart/telcopulse/values-dev.yaml --set alerting.ingestion=alertmanager --set monitoring.serviceMonitor.enabled=true --set monitoring.prometheusRule.enabled=true --set monitoring.alertmanagerConfig.enabled=true --set monitoring.alertmanagerConfig.telegram.enabled=true --set monitoring.alertmanagerConfig.telegram.chatID=-1001234567890 --set backup.enabled=true' \
  > "$render_dir/monitoring.yaml"
printf 'monitoring: '
docker run --rm -i "$schema_image" -summary -strict -kubernetes-version 1.34.0 -schema-location default -schema-location "$crd_schemas" \
  < "$render_dir/monitoring.yaml"
grep -q 'alert: PurchaseLatencyBudgetBurn' "$render_dir/monitoring.yaml" || { echo 'PrometheusRule is missing SLO alerts' >&2; exit 1; }
grep -q 'record: slo:sli_error:ratio_rate5m' "$render_dir/monitoring.yaml" || { echo 'PrometheusRule is missing Sloth recordings' >&2; exit 1; }
grep -q 'kind: CronJob' "$render_dir/monitoring.yaml" || { echo 'backup CronJob missing' >&2; exit 1; }
if grep -q 'alert: PostgresDown' "$render_dir/monitoring.yaml"; then echo 'dependency rules rendered without exporters' >&2; exit 1; fi

for invalid in \
  'monitoring.alertmanagerConfig.enabled=true' \
  'alerting.ingestion=alertmanager,monitoring.alertmanagerConfig.enabled=true,monitoring.alertmanagerConfig.telegram.enabled=true'; do
  if tar -C "$chart_root" -cf - telcopulse | docker run --rm -i --entrypoint sh "$helm_image" -c \
    "mkdir /chart && tar -xf - -C /chart && helm template telcopulse /chart/telcopulse --set '$invalid'" \
    > /dev/null 2> "$render_dir/invalid.log"; then
    printf 'expected Helm to reject %s\n' "$invalid" >&2
    exit 1
  fi
done

for example in namespace.example.yaml runtime-secret.example.yaml; do
  printf '%s: ' "$example"
  docker run --rm -i "$schema_image" -summary -strict -kubernetes-version 1.34.0 \
    < "$repo_root/infrastructure/kubernetes/$example"
done
