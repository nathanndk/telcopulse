#!/bin/sh
set -eu

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
temp_dir=$(mktemp -d)
trap 'rm -rf "$temp_dir"' EXIT

cat > "$temp_dir/docker" <<'MOCK'
#!/bin/sh
if [ "$1" = login ]; then
  # Consume the password without recording it.
  cat >/dev/null
fi
printf '%s\n' "$*" >> "$DOCKER_CALL_LOG"
if [ "$1" = inspect ]; then
  for image in "$@"; do :; done
  printf '%s@sha256:%064d\n' "${image%:*}" 0
fi
MOCK
chmod +x "$temp_dir/docker"
for tool in trivy syft cosign; do
  cat > "$temp_dir/$tool" <<MOCK
#!/bin/sh
printf '$tool %s\n' "\$*" >> "\$DOCKER_CALL_LOG"
if [ "$tool" = trivy ] && [ -n "\${MOCK_VULNERABLE:-}" ] && printf '%s' "\$*" | grep -q "/\$MOCK_VULNERABLE:"; then exit 1; fi
if [ "$tool" = syft ]; then for arg in "\$@"; do case "\$arg" in cyclonedx-json=*) echo '{}' > "\${arg#cyclonedx-json=}" ;; esac; done; fi
MOCK
  chmod +x "$temp_dir/$tool"
done

export PATH="$temp_dir:$PATH"
export DOCKER_CALL_LOG="$temp_dir/docker-calls"
export CI_COMMIT_SHA=0123456789abcdef0123456789abcdef01234567
export CI_DEFAULT_BRANCH=main
export CI_COMMIT_BRANCH=feature/test
export CI_COMMIT_REF_PROTECTED=false
export JFROG_REGISTRY=registry.example.invalid
export JFROG_IMAGE_PATH=telcopulse
export JFROG_USERNAME=ci-user
export JFROG_PASSWORD=synthetic-test-value
export COSIGN_PRIVATE_KEY=synthetic-key-material COSIGN_PASSWORD=synthetic-passphrase

if sh "$repo_root/scripts/publish-images.sh" >"$temp_dir/output" 2>&1; then
  echo 'Unprotected branch was allowed to publish' >&2
  exit 1
fi
test ! -e "$DOCKER_CALL_LOG"

export CI_COMMIT_BRANCH=main CI_COMMIT_REF_PROTECTED=true
export JFROG_IMAGE_PATH='../escape'
if sh "$repo_root/scripts/publish-images.sh" >"$temp_dir/output" 2>&1; then
  echo 'Invalid repository path was allowed' >&2
  exit 1
fi
test ! -e "$DOCKER_CALL_LOG"

export JFROG_IMAGE_PATH=telcopulse
cd "$temp_dir"
# A vulnerable image stops the job before it is pushed.
if MOCK_VULNERABLE=payment-service sh "$repo_root/scripts/publish-images.sh" >"$temp_dir/output" 2>&1; then
  echo 'Vulnerable image was published' >&2
  exit 1
fi
if grep -q '^push .*/payment-service:' "$DOCKER_CALL_LOG"; then
  echo 'Vulnerable image reached the registry' >&2
  exit 1
fi
rm -f "$DOCKER_CALL_LOG"
sh "$repo_root/scripts/publish-images.sh" >"$temp_dir/output"
test "$(grep -c '^trivy image --exit-code 1 --severity HIGH,CRITICAL' "$DOCKER_CALL_LOG")" -eq 10
test "$(grep -c '^cosign sign --yes --key env://COSIGN_PRIVATE_KEY --use-signing-config=false --tlog-upload=false .*@sha256:' "$DOCKER_CALL_LOG")" -eq 10
test "$(grep -c '^cosign attest --yes --key env://COSIGN_PRIVATE_KEY --type cyclonedx' "$DOCKER_CALL_LOG")" -eq 10
test "$(ls sbom | wc -l)" -eq 10
test "$(wc -l < release-digests.txt)" -eq 10
# Every scan happens before the matching push.
for service in auth-service web; do
  scan=$(grep -n "^trivy .*/$service:" "$DOCKER_CALL_LOG" | cut -d: -f1)
  push=$(grep -n "^push .*/$service:" "$DOCKER_CALL_LOG" | cut -d: -f1)
  test "$scan" -lt "$push"
done
if grep -q 'synthetic-key-material\|synthetic-passphrase' "$DOCKER_CALL_LOG"; then
  echo 'Signing secret leaked into command log' >&2
  exit 1
fi
test "$(grep -c '^build ' "$DOCKER_CALL_LOG")" -eq 10
test "$(grep -c '^push ' "$DOCKER_CALL_LOG")" -eq 10
test "$(grep -c '^login ' "$DOCKER_CALL_LOG")" -eq 1
test "$(grep -c '^logout ' "$DOCKER_CALL_LOG")" -eq 1
grep -q 'build --file apps/web/Dockerfile --build-arg API_URL=http://api-gateway:8080' "$DOCKER_CALL_LOG"
grep -q "push registry.example.invalid/telcopulse/web:$CI_COMMIT_SHA" "$DOCKER_CALL_LOG"
if grep -q "$JFROG_PASSWORD" "$DOCKER_CALL_LOG"; then
  echo 'Registry password leaked into command log' >&2
  exit 1
fi
echo 'Protected publication contract passed'
