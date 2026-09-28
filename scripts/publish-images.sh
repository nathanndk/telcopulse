#!/bin/sh
set -eu

# The CI job must run only on the protected default branch, after the quality
# stage. Check that boundary again here before touching registry credentials.
if [ "${CI_COMMIT_BRANCH:-}" != "${CI_DEFAULT_BRANCH:-}" ] ||
  [ -z "${CI_DEFAULT_BRANCH:-}" ] ||
  [ "${CI_COMMIT_REF_PROTECTED:-}" != "true" ]; then
  echo 'Refusing to publish outside the protected default branch' >&2
  exit 1
fi

for name in CI_COMMIT_SHA JFROG_REGISTRY JFROG_IMAGE_PATH JFROG_USERNAME JFROG_PASSWORD; do
  eval "value=\${$name:-}"
  if [ -z "$value" ]; then
    echo "Missing required CI variable: $name" >&2
    exit 1
  fi
done

case "$CI_COMMIT_SHA" in
  *[!a-f0-9]*|'') echo 'CI_COMMIT_SHA must be a lowercase commit hash' >&2; exit 1 ;;
esac
if [ "${#CI_COMMIT_SHA}" -ne 40 ]; then
  echo 'CI_COMMIT_SHA must contain 40 hexadecimal characters' >&2
  exit 1
fi
case "$JFROG_REGISTRY" in
  *[!a-zA-Z0-9.:-]*|*'..'*|.*|*.)
    echo 'JFROG_REGISTRY must be a registry host with optional port' >&2
    exit 1 ;;
esac
case "$JFROG_IMAGE_PATH" in
  *[!a-z0-9._/-]*|/*|*/|*'..'*|*'//'*)
    echo 'JFROG_IMAGE_PATH must be a lowercase repository path' >&2
    exit 1 ;;
esac

registry_path="$JFROG_REGISTRY/$JFROG_IMAGE_PATH"
printf '%s' "$JFROG_PASSWORD" | docker login "$JFROG_REGISTRY" --username "$JFROG_USERNAME" --password-stdin >/dev/null
trap 'docker logout "$JFROG_REGISTRY" >/dev/null 2>&1 || true' EXIT

for service in auth-service subscriber-service package-service payment-service notification-service incident-service simulation-service deployment-service api-gateway; do
  image="$registry_path/$service:$CI_COMMIT_SHA"
  docker build --file services/Dockerfile --build-arg "SERVICE=$service" --tag "$image" services
  docker push "$image"
done

web_image="$registry_path/web:$CI_COMMIT_SHA"
docker build --file apps/web/Dockerfile --build-arg API_URL=http://api-gateway:8080 --tag "$web_image" .
docker push "$web_image"

echo "Published ten images with immutable commit tag $CI_COMMIT_SHA"
