#!/usr/bin/env bash
# Copies the Prometheus rule files into the Helm chart, which can only read files
# inside its own directory. With --check, fails if the chart copies are stale.
set -euo pipefail
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source_dir="$repo_root/infrastructure/prometheus"
chart_dir="$repo_root/infrastructure/helm/telcopulse/files"
status=0
for file in rules.yml slo-rules.yml; do
  if [ "${1:-}" = "--check" ]; then
    cmp -s "$source_dir/$file" "$chart_dir/$file" || { echo "chart copy of $file is stale; run scripts/sync-helm-rules.sh" >&2; status=1; }
  else
    cp "$source_dir/$file" "$chart_dir/$file"
  fi
done
exit $status
