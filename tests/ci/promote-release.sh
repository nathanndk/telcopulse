#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
temp_dir=$(mktemp -d)
trap 'rm -rf "$temp_dir"' EXIT

cat > "$temp_dir/kubectl" <<'MOCK'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "$COMMAND_LOG"
if [[ "$*" == *'config get-contexts'* ]]; then printf '%s\n' mock-dev; fi
if [[ "$*" == *"get secret ${MOCK_MISSING_SECRET:-not-a-secret}"* ]]; then exit 1; fi
if [[ "$*" == *'get secret telcopulse-runtime -o json'* ]]; then
  encoded=$(printf '%s' 'abcdefghijklmnopqrstuvwxyz0123456789' | base64 | tr -d '\n')
  printf '{"data":{"SERVICE_TOKEN":"%s"}}\n' "$encoded"
fi
if [[ "$*" == *'get deployments -l app.kubernetes.io/part-of=telcopulse -o json'* ]]; then
  printf '{"items":['
  for index in {1..10}; do
    ((index == 1)) || printf ','
    printf '{"spec":{"template":{"spec":{"containers":[{"image":"registry.example.invalid/telcopulse/service:%s"}]}}}}' "${MOCK_DEPLOYED_SHA:-0123456789abcdef0123456789abcdef01234567}"
  done
  printf ']}\n'
fi
if [[ "$*" == *'port-forward --address 127.0.0.1 svc/deployment-service 18080:8080'* ]]; then
  printf 'Forwarding from 127.0.0.1:18080 -> 8080\n'
  while :; do sleep 5; done
fi
if [[ "$*" == *'exec deployment/web'* && "${MOCK_SMOKE_FAIL_ONCE:-}" == 1 && ! -e "$MOCK_SMOKE_MARKER" ]]; then
  touch "$MOCK_SMOKE_MARKER"
  exit 1
fi
if [[ "$*" == *'exec deployment/web'* && "${MOCK_SMOKE_FAIL:-}" == 1 ]]; then exit 1; fi
MOCK
cat > "$temp_dir/curl" <<'MOCK'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "$COMMAND_LOG"
for arg in "$@"; do
  if [[ "$arg" == @*/header ]]; then
    grep -Fxq 'Authorization: Bearer abcdefghijklmnopqrstuvwxyz0123456789' "${arg#@}" || exit 1
  fi
done
cat >> "$EVENT_LOG"
printf '\n' >> "$EVENT_LOG"
if [[ "${MOCK_REPORT_FAIL:-}" == 1 ]]; then exit 22; fi
MOCK
cat > "$temp_dir/helm" <<'MOCK'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "$COMMAND_LOG"
if [[ "$*" == *' status '* && "$*" == *'--output json'* ]]; then printf '{"version":4,"info":{"last_deployed":"%s"}}\n' "$MOCK_LAST_DEPLOYED"; fi
if [[ "$*" == *' history '* && "$*" == *'--output json'* ]]; then printf '[{"revision":2},{"revision":3},{"revision":4}]\n'; fi
MOCK
cat > "$temp_dir/docker" <<'MOCK'
#!/usr/bin/env bash
if [[ "$1" == login ]]; then cat >/dev/null; fi
printf '%s\n' "$*" >> "$COMMAND_LOG"
if [[ "$*" == *'manifest inspect'* && "$*" == *"${MOCK_MISSING_IMAGE:-not-a-service}:"* ]]; then exit 1; fi
MOCK
chmod +x "$temp_dir/kubectl" "$temp_dir/helm" "$temp_dir/docker" "$temp_dir/curl"

export PATH="$temp_dir:$PATH"
export COMMAND_LOG="$temp_dir/commands"
export EVENT_LOG="$temp_dir/events"
: > "$EVENT_LOG"
export KUBECONFIG="$temp_dir/kubeconfig"
touch "$KUBECONFIG"
export KUBE_CONTEXT_DEV=mock-dev
export TARGET=DEV
export ACTION=DEPLOY
export IMAGE_SHA=0123456789abcdef0123456789abcdef01234567
export REGISTRY_HOST=registry.example.invalid
export IMAGE_PATH=telcopulse
export REGISTRY_USER=ci-read
export REGISTRY_PASSWORD=synthetic-test-value
export MOCK_LAST_DEPLOYED="$(date -u +'%Y-%m-%dT%H:%M:%SZ')"

export IMAGE_SHA=bad
if bash "$repo_root/scripts/promote-release.sh" >"$temp_dir/output" 2>&1; then
  echo 'Invalid image SHA was accepted' >&2; exit 1
fi
if grep -q ' upgrade ' "$COMMAND_LOG"; then echo 'Invalid SHA reached Helm' >&2; exit 1; fi

export IMAGE_SHA=0123456789abcdef0123456789abcdef01234567
export MOCK_MISSING_SECRET=telcopulse-registry-pull
if bash "$repo_root/scripts/promote-release.sh" >"$temp_dir/output" 2>&1; then
  echo 'Missing registry pull Secret was accepted' >&2; exit 1
fi
if grep -q ' upgrade ' "$COMMAND_LOG"; then echo 'Missing pull Secret reached Helm' >&2; exit 1; fi
unset MOCK_MISSING_SECRET
export MOCK_MISSING_IMAGE=web
if bash "$repo_root/scripts/promote-release.sh" >"$temp_dir/output" 2>&1; then
  echo 'Incomplete image set was accepted' >&2; exit 1
fi
if grep -q ' upgrade ' "$COMMAND_LOG"; then echo 'Missing image reached Helm' >&2; exit 1; fi

unset MOCK_MISSING_IMAGE
: > "$COMMAND_LOG"
export ACTION=PREFLIGHT_DEPLOY
bash "$repo_root/scripts/promote-release.sh" >"$temp_dir/output"
test "$(grep -c '^manifest inspect ' "$COMMAND_LOG")" -eq 10
test ! -s "$EVENT_LOG"
if grep -q ' upgrade ' "$COMMAND_LOG"; then echo 'Preflight changed a release' >&2; exit 1; fi

: > "$COMMAND_LOG"
export ACTION=DEPLOY
bash "$repo_root/scripts/promote-release.sh" >"$temp_dir/output"
grep -q ' upgrade --install telcopulse ' "$COMMAND_LOG"
grep -q 'image.tag=0123456789abcdef0123456789abcdef01234567' "$COMMAND_LOG"
grep -q 'imagePullSecrets\[0\].name=telcopulse-registry-pull' "$COMMAND_LOG"
grep -q 'get secret telcopulse-registry-pull' "$COMMAND_LOG"
grep -q 'exec deployment/web -- node -e' "$COMMAND_LOG"
grep -q 'port-forward --address 127.0.0.1 svc/deployment-service 18080:8080' "$COMMAND_LOG"
test "$(grep -c '^manifest inspect ' "$COMMAND_LOG")" -eq 10
test "$(wc -l < "$EVENT_LOG" | tr -d ' ')" -eq 10
jq -es --arg stamp "$MOCK_LAST_DEPLOYED" 'length == 10 and ([.[].service] | unique | length == 10) and all(.[]; .environment == "development" and .status == "Completed" and .deployer == "ci:jenkins-deploy" and .version == "0123456789abcdef0123456789abcdef01234567" and .commit_sha == "0123456789abcdef0123456789abcdef01234567" and .occurred_at == $stamp and (.event_id | contains("-rev4-")) and (.deployment_id | contains("-rev4-")))' "$EVENT_LOG" | grep -Fxq true
if grep -Fq 'abcdefghijklmnopqrstuvwxyz0123456789' "$COMMAND_LOG" "$temp_dir/output"; then
  echo 'Service token leaked into command output' >&2; exit 1
fi

: > "$COMMAND_LOG"
: > "$EVENT_LOG"
export MOCK_SMOKE_FAIL_ONCE=1 MOCK_SMOKE_MARKER="$temp_dir/smoke-failed"
if bash "$repo_root/scripts/promote-release.sh" >"$temp_dir/output" 2>&1; then
  echo 'Failed smoke was accepted' >&2; exit 1
fi
grep -q ' rollback telcopulse 4 --wait' "$COMMAND_LOG"
test "$(grep -c 'exec deployment/web -- node -e' "$COMMAND_LOG")" -eq 2
test "$(wc -l < "$EVENT_LOG" | tr -d ' ')" -eq 10
jq -es 'length == 10 and all(.[]; .status == "Failed" and .deployer == "ci:jenkins-deploy")' "$EVENT_LOG" | grep -Fxq true

: > "$COMMAND_LOG"
: > "$EVENT_LOG"
unset MOCK_SMOKE_FAIL_ONCE
export MOCK_DEPLOYED_SHA=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
if bash "$repo_root/scripts/promote-release.sh" >"$temp_dir/output" 2>&1; then
  echo 'Wrong deployed image SHA was accepted' >&2; exit 1
fi
grep -q ' rollback telcopulse 4 --wait' "$COMMAND_LOG"
test "$(wc -l < "$EVENT_LOG" | tr -d ' ')" -eq 10
jq -es 'length == 10 and all(.[]; .status == "Failed" and .deployer == "ci:jenkins-deploy")' "$EVENT_LOG" | grep -Fxq true
unset MOCK_DEPLOYED_SHA

: > "$COMMAND_LOG"
: > "$EVENT_LOG"
export MOCK_REPORT_FAIL=1
if bash "$repo_root/scripts/promote-release.sh" >"$temp_dir/output" 2>&1; then
  echo 'Failed release reporting was accepted' >&2; exit 1
fi
grep -q 'Deployment event reporting failed' "$temp_dir/output"
if grep -q ' rollback telcopulse ' "$COMMAND_LOG"; then
  echo 'Healthy release was rolled back for reporting failure' >&2; exit 1
fi
unset MOCK_REPORT_FAIL

: > "$COMMAND_LOG"
export ACTION=PREFLIGHT_ROLLBACK ROLLBACK_REVISION=2
bash "$repo_root/scripts/promote-release.sh" >"$temp_dir/output"
if grep -q ' rollback ' "$COMMAND_LOG"; then echo 'Rollback preflight changed a release' >&2; exit 1; fi

: > "$COMMAND_LOG"
export ROLLBACK_REVISION=9
if bash "$repo_root/scripts/promote-release.sh" >"$temp_dir/output" 2>&1; then
  echo 'Unknown rollback revision was accepted' >&2; exit 1
fi
if grep -q ' rollback ' "$COMMAND_LOG"; then echo 'Unknown revision changed a release' >&2; exit 1; fi

: > "$COMMAND_LOG"
: > "$EVENT_LOG"
export ACTION=ROLLBACK ROLLBACK_REVISION=2
bash "$repo_root/scripts/promote-release.sh" >"$temp_dir/output"
grep -q ' rollback telcopulse 2 --wait' "$COMMAND_LOG"
test "$(wc -l < "$EVENT_LOG" | tr -d ' ')" -eq 10
jq -es 'length == 10 and all(.[]; .environment == "development" and .status == "Completed" and .deployer == "ci:jenkins-rollback" and .version == "rollback-rev-2")' "$EVENT_LOG" | grep -Fxq true

: > "$COMMAND_LOG"
: > "$EVENT_LOG"
export MOCK_SMOKE_FAIL_ONCE=1 MOCK_SMOKE_MARKER="$temp_dir/rollback-smoke-failed"
if bash "$repo_root/scripts/promote-release.sh" >"$temp_dir/output" 2>&1; then
  echo 'Failed rollback smoke was accepted' >&2; exit 1
fi
grep -q ' rollback telcopulse 4 --wait' "$COMMAND_LOG"
test "$(grep -c 'exec deployment/web -- node -e' "$COMMAND_LOG")" -eq 2
test "$(wc -l < "$EVENT_LOG" | tr -d ' ')" -eq 10
jq -es 'length == 10 and all(.[]; .status == "Failed" and .deployer == "ci:jenkins-rollback" and .version == "rollback-rev-2")' "$EVENT_LOG" | grep -Fxq true
unset MOCK_SMOKE_FAIL_ONCE

: > "$COMMAND_LOG"
export ACTION=SMOKE
unset REGISTRY_PASSWORD
bash "$repo_root/scripts/promote-release.sh" >"$temp_dir/output"
if grep -q '^login ' "$COMMAND_LOG"; then echo 'Smoke requested registry credentials' >&2; exit 1; fi

: > "$COMMAND_LOG"
export TARGET=PRODUCTION_LIKE KUBE_CONTEXT_PRODUCTION_LIKE=mock-dev
export ACTION=ROLLBACK ROLLBACK_REVISION=3
bash "$repo_root/scripts/promote-release.sh" >"$temp_dir/output"
grep -q 'get namespace telcopulse-eval-production' "$COMMAND_LOG"
grep -q ' rollback telcopulse 3 --wait' "$COMMAND_LOG"
if grep -q 'get namespace telcopulse-production' "$COMMAND_LOG"; then
  echo 'Production-like action targeted a production namespace' >&2; exit 1
fi
echo 'Promotion and rollback contract passed'
