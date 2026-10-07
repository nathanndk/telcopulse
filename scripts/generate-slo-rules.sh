#!/usr/bin/env bash
# Renders infrastructure/slo/*.yml with a pinned Sloth image into Prometheus
# recording rules. With --check, fails if the committed output is stale.
set -euo pipefail
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
image=ghcr.io/slok/sloth:v0.16.0
spec="$repo_root/infrastructure/slo/telcopulse.yml"
out="$repo_root/infrastructure/prometheus/slo-rules.yml"
generated="$(docker run --rm -v "$(dirname "$spec"):/slo:ro" "$image" generate --no-log --disable-alerts -i "/slo/$(basename "$spec")")"
if [ "${1:-}" = "--check" ]; then
  if ! diff -u "$out" <(printf '%s\n' "$generated"); then
    echo "slo-rules.yml is stale; run scripts/generate-slo-rules.sh" >&2
    exit 1
  fi
  echo "slo-rules.yml is current"
else
  printf '%s\n' "$generated" > "$out"
  echo "wrote ${out#"$repo_root"/}"
fi
