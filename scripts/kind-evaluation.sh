#!/usr/bin/env bash
# Local Kubernetes evaluation of TelcoPulse on kind.
#   up      create the cluster and platform add-ons with OpenTofu
#   deploy  build and side-load images, create secrets and dependencies, and let
#           Argo CD sync the chart from this checkout (DEPLOY_WITH=helm skips Argo CD)
#   down    destroy the cluster and the git-daemon container
# Needs Docker, OpenTofu, kind, kubectl, helm and jq. Generated credentials are kept
# in the ignored .secrets/kind-runtime.env so repeated deploys reuse them.
set -euo pipefail
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"
tf_dir=infrastructure/terraform/kind
cluster=telcopulse
context="kind-$cluster"
namespace=telcopulse
services=(api-gateway auth-service subscriber-service package-service payment-service notification-service incident-service simulation-service deployment-service web)
k() { kubectl --context "$context" "$@"; }

up() {
  tofu -chdir="$tf_dir" init -input=false >/dev/null
  tofu -chdir="$tf_dir" apply -input=false -auto-approve
  # Let the control plane run workloads so draining the worker has a destination.
  k taint nodes "$cluster-control-plane" node-role.kubernetes.io/control-plane:NoSchedule- 2>/dev/null || true
  k get nodes
}

secrets_file() {
  local file=.secrets/kind-runtime.env
  umask 077
  mkdir -p .secrets
  if [[ ! -f "$file" ]]; then
    {
      printf 'POSTGRES_PASSWORD=%s\n' "$(openssl rand -hex 24)"
      for name in SERVICE_TOKEN INCIDENT_OPERATOR_TOKEN SIMULATION_OPERATOR_TOKEN NOTIFICATION_OPERATOR_TOKEN ALERT_WEBHOOK_TOKEN; do
        printf '%s=%s\n' "$name" "$(openssl rand -hex 32)"
      done
      printf 'AUTH_BOOTSTRAP_PASSWORD=%s\n' "$(openssl rand -hex 24)"
    } > "$file"
  fi
  echo "$file"
}

load_images() {
  docker compose build "${services[@]}" >/dev/null
  for service in "${services[@]}"; do
    docker tag "telcopulse-$service:latest" "localhost/telcopulse/$service:kind"
  done
  kind load docker-image --name "$cluster" $(printf 'localhost/telcopulse/%s:kind ' "${services[@]}")
}

git_daemon() {
  # Serves this checkout (committed HEAD only) to Argo CD on the kind network.
  docker rm -f telcopulse-git >/dev/null 2>&1 || true
  docker run -d --name telcopulse-git --network kind -v "$repo_root:/repo:ro" alpine/git:2.54.0 \
    -c 'safe.directory=*' daemon --reuseaddr --export-all --base-path=/ /repo >/dev/null
}

deploy() {
  local env_file
  env_file=$(secrets_file)
  # shellcheck disable=SC1090
  source "$env_file"
  load_images
  k apply -f infrastructure/kubernetes/kind/dependencies.yaml >/dev/null
  k -n telcopulse-deps create secret generic postgres-credentials --from-literal=password="$POSTGRES_PASSWORD" --dry-run=client -o yaml | k apply -f - >/dev/null
  k -n telcopulse-deps rollout status statefulset/postgres --timeout=300s
  k -n telcopulse-deps rollout status statefulset/kafka --timeout=300s
  k create namespace "$namespace" --dry-run=client -o yaml | k apply -f - >/dev/null
  k create namespace telcopulse-load --dry-run=client -o yaml | k apply -f - >/dev/null
  local telegram=()
  if [[ -n "${TELEGRAM_BOT_TOKEN:-}" ]]; then telegram=(--from-literal=TELEGRAM_BOT_TOKEN="$TELEGRAM_BOT_TOKEN"); fi
  k -n "$namespace" create secret generic telcopulse-runtime \
    --from-literal=DATABASE_URL="postgres://telcopulse:$POSTGRES_PASSWORD@postgres.telcopulse-deps.svc:5432/telcopulse?sslmode=disable" \
    --from-literal=SERVICE_TOKEN="$SERVICE_TOKEN" --from-literal=INCIDENT_OPERATOR_TOKEN="$INCIDENT_OPERATOR_TOKEN" \
    --from-literal=SIMULATION_OPERATOR_TOKEN="$SIMULATION_OPERATOR_TOKEN" --from-literal=NOTIFICATION_OPERATOR_TOKEN="$NOTIFICATION_OPERATOR_TOKEN" \
    --from-literal=ALERT_WEBHOOK_TOKEN="$ALERT_WEBHOOK_TOKEN" --from-literal=AUTH_BOOTSTRAP_PASSWORD="$AUTH_BOOTSTRAP_PASSWORD" "${telegram[@]}" \
    --dry-run=client -o yaml | k apply -f - >/dev/null
  k -n telcopulse-load create secret generic load-credentials --from-literal=ADMIN_PASSWORD="$AUTH_BOOTSTRAP_PASSWORD" \
    --dry-run=client -o yaml | k apply -f - >/dev/null
  if [[ "${DEPLOY_WITH:-argocd}" == helm ]]; then
    helm --kube-context "$context" upgrade --install telcopulse infrastructure/helm/telcopulse -n "$namespace" \
      -f infrastructure/helm/telcopulse/values-kind.yaml --wait --timeout 15m
  else
    git_daemon
    k apply -f infrastructure/gitops/project.yaml -f infrastructure/gitops/telcopulse-kind.yaml >/dev/null
    k -n argocd wait --for=jsonpath='{.status.sync.status}'=Synced application/telcopulse-kind --timeout=900s
    k -n argocd wait --for=jsonpath='{.status.health.status}'=Healthy application/telcopulse-kind --timeout=900s
  fi
  k -n "$namespace" get pods -o wide
}

down() {
  docker rm -f telcopulse-git >/dev/null 2>&1 || true
  tofu -chdir="$tf_dir" destroy -input=false -auto-approve
}

case "${1:-}" in
  up) up ;;
  deploy) deploy ;;
  down) down ;;
  *) echo "usage: $0 up|deploy|down" >&2; exit 1 ;;
esac
