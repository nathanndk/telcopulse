#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
chart_root="$repo_root/infrastructure/helm"
render_dir="$(mktemp -d)"
trap 'rm -rf "$render_dir"' EXIT

helm_image=alpine/helm:3.22.0
schema_image=ghcr.io/yannh/kubeconform:v0.7.0

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
ruby "$repo_root/scripts/check-helm-vendors.rb" "$render_dir/dev.yaml" "$render_dir/vendors.yaml"

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

for example in namespace.example.yaml runtime-secret.example.yaml; do
  printf '%s: ' "$example"
  docker run --rm -i "$schema_image" -summary -strict -kubernetes-version 1.34.0 \
    < "$repo_root/infrastructure/kubernetes/$example"
done
