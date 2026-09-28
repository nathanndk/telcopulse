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
MOCK
chmod +x "$temp_dir/docker"

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
sh "$repo_root/scripts/publish-images.sh" >"$temp_dir/output"
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
