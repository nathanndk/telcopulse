#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
release=telcopulse
chart="$repo_root/infrastructure/helm/telcopulse"

die() { printf '%s\n' "$*" >&2; exit 1; }

case "${ACTION:-}" in
  DEPLOY|ROLLBACK|SMOKE|PREFLIGHT_DEPLOY|PREFLIGHT_ROLLBACK) ;;
  *) die 'ACTION must be DEPLOY, ROLLBACK, SMOKE, or a preflight action' ;;
esac

case "${TARGET:-}" in
  DEV) profile=dev; namespace=telcopulse-dev; context=${KUBE_CONTEXT_DEV:-}; tls_secret=telcopulse-dev-tls; report_environment=development ;;
  STAGING) profile=staging; namespace=telcopulse-staging; context=${KUBE_CONTEXT_STAGING:-}; tls_secret=telcopulse-staging-tls; report_environment=staging ;;
  PRODUCTION_LIKE) profile=production; namespace=telcopulse-eval-production; context=${KUBE_CONTEXT_PRODUCTION_LIKE:-}; tls_secret=telcopulse-tls; report_environment=production ;;
  *) die 'TARGET must be DEV, STAGING, or PRODUCTION_LIKE' ;;
esac

[[ -n "$context" ]] || die "Kubernetes context for $TARGET is required"
[[ -r "${KUBECONFIG:-}" ]] || die 'KUBECONFIG must name a readable credential file'
command -v helm >/dev/null || die 'helm is required'
command -v kubectl >/dev/null || die 'kubectl is required'
command -v jq >/dev/null || die 'jq is required'
if [[ "$ACTION" == DEPLOY || "$ACTION" == ROLLBACK ]]; then
  command -v curl >/dev/null || die 'curl is required to report verified release events'
  command -v base64 >/dev/null || die 'base64 is required to read the runtime Secret'
fi

# Never fall back to the current/default cluster or an automatically created namespace.
kubectl config get-contexts "$context" -o name | grep -Fxq -- "$context" || die "Unknown Kubernetes context: $context"
kubectl --context "$context" get namespace "$namespace" >/dev/null || die "Namespace $namespace does not exist"
kubectl --context "$context" -n "$namespace" get secret telcopulse-runtime >/dev/null || die 'Runtime Secret is missing'
kubectl --context "$context" -n "$namespace" get secret "$tls_secret" >/dev/null || die 'Ingress TLS Secret is missing'
if [[ "$ACTION" == DEPLOY || "$ACTION" == PREFLIGHT_DEPLOY ]]; then
  kubectl --context "$context" -n "$namespace" get secret telcopulse-registry-pull >/dev/null || die 'Registry pull Secret is missing'
fi

helm_cmd=(helm --kube-context "$context" --namespace "$namespace")
kubectl_cmd=(kubectl --context "$context" -n "$namespace")

current_revision() {
  "${helm_cmd[@]}" status "$release" --output json | jq -er '.version | numbers'
}

smoke() {
  "${kubectl_cmd[@]}" get deployments -l app.kubernetes.io/part-of=telcopulse -o json |
    jq -e '.items | length == 10' >/dev/null || return 1
  "${kubectl_cmd[@]}" wait --for=condition=available deployment -l app.kubernetes.io/part-of=telcopulse --timeout=300s || return 1
  "${kubectl_cmd[@]}" exec deployment/web -- node -e '
    (async () => {
      const root = await fetch("http://127.0.0.1:3000/");
      if (!root.ok) throw new Error(`web status ${root.status}`);
      const response = await fetch("http://127.0.0.1:3000/api/v1/auth/status");
      if (!response.ok || (await response.json()).required !== true) {
        throw new Error(`protected API status ${response.status}`);
      }
      console.log("web and protected gateway smoke passed");
    })().catch(error => { console.error(error); process.exit(1); });
  ' || return 1
}

deployed_sha() {
  "${kubectl_cmd[@]}" get deployments -l app.kubernetes.io/part-of=telcopulse -o json |
    jq -er '.items | map(.spec.template.spec.containers[0].image | split(":")[-1]) | unique | if length == 1 and (.[0] | test("^[0-9a-f]{40}$")) then .[0] else error("release images do not share one full SHA") end'
}

# Record a verified release outcome for each service. The port-forward stays
# local to the trusted runner; credentials never enter command arguments or logs.
report_release() (
  set -euo pipefail
  local status=$1 operation=$2 sha=$3 version=$4
  [[ "$sha" =~ ^[0-9a-f]{40}$ ]] || die 'Cannot report a release without a full image SHA'
  local report_dir forward_pid token encoded service payload event_id deployment_id stamp revision release_json
  if [[ $# -ge 5 ]]; then
    release_json=$5
  else
    release_json=$("${helm_cmd[@]}" status "$release" --output json) || die 'Cannot read release metadata for reporting'
  fi
  revision=$(printf '%s' "$release_json" | jq -er '.version | numbers') || die 'Release revision is missing'
  stamp=$(printf '%s' "$release_json" | jq -er '.info.last_deployed | strings') || die 'Release deployment time is missing'
  [[ "$revision" =~ ^[1-9][0-9]*$ && "$stamp" =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}T ]] || die 'Invalid release metadata for reporting'
  report_dir=$(mktemp -d)
  umask 077
  trap 'if [[ -n "${forward_pid:-}" ]]; then kill "$forward_pid" 2>/dev/null || true; wait "$forward_pid" 2>/dev/null || true; fi; rm -rf "$report_dir"' EXIT
  encoded=$("${kubectl_cmd[@]}" get secret telcopulse-runtime -o json | jq -er '.data.SERVICE_TOKEN')
  token=$(printf '%s' "$encoded" | base64 -d) || die 'Cannot decode service token'
  [[ ${#token} -ge 32 && "$token" != *$'\n'* && "$token" != *$'\r'* ]] || die 'Invalid service token for deployment reporting'
  printf 'Authorization: Bearer %s\n' "$token" > "$report_dir/header"
  unset token encoded
  "${kubectl_cmd[@]}" port-forward --address 127.0.0.1 svc/deployment-service 18080:8080 > "$report_dir/port-forward.log" 2>&1 &
  forward_pid=$!
  for _ in 1 2 3 4 5 6 7 8 9 10; do
    kill -0 "$forward_pid" 2>/dev/null || die 'Deployment-service port-forward stopped'
    if grep -Fq 'Forwarding from 127.0.0.1:18080' "$report_dir/port-forward.log"; then break; fi
    sleep 1
  done
  grep -Fq 'Forwarding from 127.0.0.1:18080' "$report_dir/port-forward.log" || die 'Deployment-service port-forward did not become ready'
  for service in auth-service subscriber-service package-service payment-service notification-service incident-service simulation-service deployment-service api-gateway web; do
    deployment_id="ci-deploy-${TARGET}-rev${revision}-${service}"
    event_id="ci-event-${TARGET}-rev${revision}-${service}-${status}"
    payload=$(jq -nc --arg event_id "$event_id" --arg deployment_id "$deployment_id" \
      --arg service "$service" --arg version "$version" --arg commit_sha "$sha" \
      --arg environment "$report_environment" --arg deployer "ci:jenkins-${operation}" \
      --arg status "$status" --arg occurred_at "$stamp" \
      '{event_id:$event_id,deployment_id:$deployment_id,service:$service,version:$version,commit_sha:$commit_sha,environment:$environment,deployer:$deployer,status:$status,occurred_at:$occurred_at}')
    printf '%s' "$payload" | curl --fail --silent --show-error --max-time 10 --retry 2 --retry-all-errors \
      -o /dev/null -X POST -H @"$report_dir/header" -H 'Content-Type: application/json' \
      --data-binary @- 'http://127.0.0.1:18080/internal/deployments/events' || die "Deployment event reporting failed for $service"
  done
  printf 'Reported %s %s release events for %s\n' "$operation" "$status" "$TARGET"
)

case "$ACTION" in
  SMOKE)
    "${helm_cmd[@]}" status "$release" >/dev/null || die 'No deployed Helm release to check'
    smoke
    ;;
  ROLLBACK|PREFLIGHT_ROLLBACK)
    [[ "${ROLLBACK_REVISION:-}" =~ ^[1-9][0-9]*$ ]] || die 'ROLLBACK_REVISION must be a positive Helm revision'
    previous_revision=$(current_revision) || die 'No deployed Helm release to roll back'
    "${helm_cmd[@]}" history "$release" --output json |
      jq -e --arg revision "$ROLLBACK_REVISION" 'any(.[]; (.revision | tostring) == $revision)' >/dev/null || die 'Requested Helm revision is not in release history'
    if [[ "$ACTION" == PREFLIGHT_ROLLBACK ]]; then
      printf 'Rollback preflight passed: %s revision %s -> %s\n' "$TARGET" "$previous_revision" "$ROLLBACK_REVISION"
      exit 0
    fi
    "${helm_cmd[@]}" rollback "$release" "$ROLLBACK_REVISION" --wait --timeout 10m
    if ! smoke; then
      failed_release_json=$("${helm_cmd[@]}" status "$release" --output json 2>/dev/null || true)
      failed_sha=$(deployed_sha 2>/dev/null || true)
      printf 'Rollback smoke failed; restoring revision %s\n' "$previous_revision" >&2
      "${helm_cmd[@]}" rollback "$release" "$previous_revision" --wait --timeout 10m || die 'Rollback recovery failed; inspect cluster health'
      smoke || die 'Previous revision failed smoke after rollback recovery'
      if [[ -n "$failed_release_json" && -n "$failed_sha" ]]; then
        report_release Failed rollback "$failed_sha" "rollback-rev-$ROLLBACK_REVISION" "$failed_release_json" || printf 'Warning: failed rollback event could not be recorded\n' >&2
      fi
      die 'Requested rollback failed smoke; previous revision restored'
    fi
    rollback_sha=$(deployed_sha) || die 'Rolled-back release images do not share a full commit SHA'
    report_release Completed rollback "$rollback_sha" "rollback-rev-$ROLLBACK_REVISION"
    ;;
  DEPLOY|PREFLIGHT_DEPLOY)
    [[ "${IMAGE_SHA:-}" =~ ^[0-9a-f]{40}$ ]] || die 'IMAGE_SHA must be a full lowercase 40-character commit SHA'
    [[ "${REGISTRY_HOST:-}" =~ ^[A-Za-z0-9.:-]+$ && "$REGISTRY_HOST" != *..* ]] || die 'REGISTRY_HOST must be a registry host with optional port'
    [[ "${IMAGE_PATH:-}" =~ ^[a-z0-9._/-]+$ && "$IMAGE_PATH" != /* && "$IMAGE_PATH" != */ && "$IMAGE_PATH" != *..* && "$IMAGE_PATH" != *//* ]] || die 'IMAGE_PATH must be a lowercase repository path'
    [[ -n "${REGISTRY_USER:-}" && -n "${REGISTRY_PASSWORD:-}" ]] || die 'Registry read credentials are required'
    command -v docker >/dev/null || die 'Docker CLI is required for image manifest inspection'
    registry_path="$REGISTRY_HOST/$IMAGE_PATH"
    docker_config_dir=$(mktemp -d)
    export DOCKER_CONFIG="$docker_config_dir"
    trap 'docker logout "$REGISTRY_HOST" >/dev/null 2>&1 || true; rm -rf "$docker_config_dir"' EXIT
    printf '%s' "$REGISTRY_PASSWORD" | docker login "$REGISTRY_HOST" --username "$REGISTRY_USER" --password-stdin >/dev/null
    for service in auth-service subscriber-service package-service payment-service notification-service incident-service simulation-service deployment-service api-gateway web; do
      docker manifest inspect "$registry_path/$service:$IMAGE_SHA" >/dev/null || die "Missing published image for $service"
    done
    if [[ "$ACTION" == PREFLIGHT_DEPLOY ]]; then
      printf 'Deployment preflight passed: %s, ten images at %s\n' "$TARGET" "$IMAGE_SHA"
      exit 0
    fi
    previous_revision=$(current_revision 2>/dev/null || true)
    "${helm_cmd[@]}" upgrade --install "$release" "$chart" \
      -f "$chart/values-$profile.yaml" \
      --set-string "image.registry=$registry_path" \
      --set-string "image.tag=$IMAGE_SHA" \
      --set-string 'imagePullSecrets[0].name=telcopulse-registry-pull' \
      --atomic --wait --timeout 10m --history-max 20
    if ! smoke || [[ "$(deployed_sha)" != "$IMAGE_SHA" ]]; then
      failed_release_json=$("${helm_cmd[@]}" status "$release" --output json 2>/dev/null || true)
      if [[ -n "$previous_revision" ]]; then
        printf 'Post-deploy smoke failed; restoring revision %s\n' "$previous_revision" >&2
        "${helm_cmd[@]}" rollback "$release" "$previous_revision" --wait --timeout 10m || die 'Automatic rollback failed; inspect cluster health'
        smoke || die 'Previous revision failed smoke after automatic rollback'
        if [[ -n "$failed_release_json" ]]; then
          report_release Failed deploy "$IMAGE_SHA" "$IMAGE_SHA" "$failed_release_json" || printf 'Warning: failed deployment event could not be recorded\n' >&2
        fi
        die 'Post-deploy smoke failed; previous revision restored'
      else
        printf 'First-install smoke failed; removing failed release\n' >&2
        if [[ -n "$failed_release_json" ]]; then
          report_release Failed deploy "$IMAGE_SHA" "$IMAGE_SHA" "$failed_release_json" || printf 'Warning: failed deployment event could not be recorded\n' >&2
        fi
        "${helm_cmd[@]}" uninstall "$release" --wait --timeout 10m || die 'First-install cleanup failed; inspect cluster health'
        die 'First-install smoke failed; release removed'
      fi
    fi
    report_release Completed deploy "$IMAGE_SHA" "$IMAGE_SHA"
    ;;
esac

printf '%s %s succeeded for %s\n' "$ACTION" "$release" "$TARGET"
