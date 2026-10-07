#!/usr/bin/env bash
# Error budget release gate; see docs/error-budget-policy.md. Blocks a deploy to
# STAGING or PRODUCTION_LIKE while an SLO burn alert fires or any SLO has used its
# budget in the environment that target serves. DEV is advisory. The gate fails
# closed when Prometheus cannot answer or the SLO recordings are missing.
set -euo pipefail

die() { printf 'error-budget-gate: %s\n' "$*" >&2; exit 1; }

case "${TARGET:-}" in
  DEV) environment=development; enforced=0 ;;
  STAGING) environment=staging; enforced=1 ;;
  # The production-like evaluation release serves the staging data scope.
  PRODUCTION_LIKE) environment=staging; enforced=1 ;;
  *) die 'TARGET must be DEV, STAGING, or PRODUCTION_LIKE' ;;
esac
[[ "${SLO_PROMETHEUS_URL:-}" =~ ^https?://[A-Za-z0-9.:-]+$ ]] || die 'SLO_PROMETHEUS_URL must be an http(s) origin without credentials or path'
command -v curl >/dev/null || die 'curl is required'
command -v jq >/dev/null || die 'jq is required'

query() {
  curl --fail --silent --show-error --max-time 10 --get --data-urlencode "query=$1" "$SLO_PROMETHEUS_URL/api/v1/query" |
    jq -ec 'select(.status == "success") | .data.result'
}

burn_alerts='BusinessSuccessRateLow|PurchaseLatencyBudgetBurn|NotificationFreshnessBudgetBurn'
firing=$(query "ALERTS{alertstate=\"firing\",alertname=~\"$burn_alerts\",environment=\"$environment\"}") ||
  die "Prometheus did not answer; refusing to promote without SLO evidence"
budgets=$(query "slo:period_error_budget_remaining:ratio{environment=\"$environment\"}") ||
  die "Prometheus did not answer; refusing to promote without SLO evidence"
objectives=$(query 'slo:objective:ratio{sloth_service="telcopulse"}') ||
  die "Prometheus did not answer; refusing to promote without SLO evidence"

reasons=()
while read -r alert; do
  [[ -n "$alert" ]] && reasons+=("burn alert firing: $alert")
done < <(jq -r '.[].metric.alertname' <<<"$firing" | sort -u)

if [[ "$(jq 'length' <<<"$objectives")" == 0 ]]; then
  # Missing objectives mean the SLO rules are not deployed, not that the budget is healthy.
  reasons+=("no SLO recording rules loaded in Prometheus")
fi
# Idle windows are not recorded, so an SLO without completed traffic in the
# retained window has an objective but no budget series.
while read -r slo; do
  [[ -n "$slo" ]] && printf '%s (%s): budget unknown, no completed traffic in the retained window\n' "$slo" "$environment"
done < <(jq -r --argjson budgets "$budgets" '[.[].metric.sloth_slo] - [$budgets[].metric.sloth_slo] | unique[]' <<<"$objectives")
while IFS=$'\t' read -r slo value; do
  case "$value" in
    NaN|+Inf|-Inf) printf '%s (%s): budget unknown, no completed traffic in the retained window\n' "$slo" "$environment"; continue ;;
  esac
  printf '%s (%s): %s%% of error budget remaining\n' "$slo" "$environment" "$(awk -v v="$value" 'BEGIN { printf "%.1f", v * 100 }')"
  if awk -v v="$value" 'BEGIN { exit !(v <= 0) }'; then
    reasons+=("$slo error budget exhausted")
  fi
done < <(jq -r '.[] | [.metric.sloth_slo, .value[1]] | @tsv' <<<"$budgets")

if ((${#reasons[@]} == 0)); then
  printf 'Error budget gate passed for %s (%s)\n' "$TARGET" "$environment"
  exit 0
fi
printf 'Error budget gate finding: %s\n' "${reasons[@]}" >&2
if ((enforced == 0)); then
  printf 'DEV promotion is advisory; continuing\n' >&2
  exit 0
fi
override="${BUDGET_OVERRIDE_REASON:-}"
if [[ -n "$override" ]]; then
  [[ ${#override} -ge 20 && "$override" != *$'\n'* ]] || die 'BUDGET_OVERRIDE_REASON must be one line of at least 20 characters naming the fix or incident'
  printf 'ERROR BUDGET OVERRIDE for %s by %s: %s\n' "$TARGET" "${BUILD_USER_ID:-${USER:-unknown}}" "$override" >&2
  exit 0
fi
die "promotion to $TARGET blocked by the error budget policy; ship a reliability fix with BUDGET_OVERRIDE_REASON or wait for the budget to recover"
