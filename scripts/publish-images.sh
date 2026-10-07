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

for name in CI_COMMIT_SHA JFROG_REGISTRY JFROG_IMAGE_PATH JFROG_USERNAME JFROG_PASSWORD COSIGN_PRIVATE_KEY COSIGN_PASSWORD; do
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

for tool in trivy syft cosign; do
  command -v "$tool" >/dev/null || { echo "$tool is required for image scanning, SBOM and signing" >&2; exit 1; }
done

# Private images: do not publish signature metadata to the public Rekor log
# unless explicitly requested; verification then skips the log check.
case "${COSIGN_TRANSPARENCY_LOG:-off}" in
  off) tlog='--use-signing-config=false --tlog-upload=false' ;;
  public) tlog='' ;;
  *) echo 'COSIGN_TRANSPARENCY_LOG must be off or public' >&2; exit 1 ;;
esac

registry_path="$JFROG_REGISTRY/$JFROG_IMAGE_PATH"
printf '%s' "$JFROG_PASSWORD" | docker login "$JFROG_REGISTRY" --username "$JFROG_USERNAME" --password-stdin >/dev/null
trap 'docker logout "$JFROG_REGISTRY" >/dev/null 2>&1 || true' EXIT
mkdir -p sbom
: > release-digests.txt

# Scan before push: an image with a fixable HIGH or CRITICAL vulnerability never
# reaches the registry. After push, sign the digest (not the mutable tag) and
# attach the CycloneDX SBOM as a signed attestation. cosign reads the key from
# COSIGN_PRIVATE_KEY and its passphrase from COSIGN_PASSWORD.
publish() {
  service=$1
  image="$registry_path/$service:$CI_COMMIT_SHA"
  trivy image --exit-code 1 --severity HIGH,CRITICAL --ignore-unfixed --no-progress --scanners vuln "$image"
  docker push "$image"
  digest=$(docker inspect --format '{{index .RepoDigests 0}}' "$image")
  case "$digest" in
    "$registry_path/$service@sha256:"*) ;;
    *) echo "unexpected pushed digest for $service: $digest" >&2; exit 1 ;;
  esac
  syft scan "docker:$image" --output "cyclonedx-json=sbom/$service.cdx.json"
  # shellcheck disable=SC2086
  cosign sign --yes --key env://COSIGN_PRIVATE_KEY $tlog "$digest"
  # shellcheck disable=SC2086
  cosign attest --yes --key env://COSIGN_PRIVATE_KEY --type cyclonedx --predicate "sbom/$service.cdx.json" $tlog "$digest"
  echo "$digest" >> release-digests.txt
}

for service in auth-service subscriber-service package-service payment-service notification-service incident-service simulation-service deployment-service api-gateway; do
  docker build --file services/Dockerfile --build-arg "SERVICE=$service" --tag "$registry_path/$service:$CI_COMMIT_SHA" services
  publish "$service"
done

docker build --file apps/web/Dockerfile --build-arg API_URL=http://api-gateway:8080 --tag "$registry_path/web:$CI_COMMIT_SHA" .
publish web

echo "Published, scanned, signed and attested ten images with immutable commit tag $CI_COMMIT_SHA"
