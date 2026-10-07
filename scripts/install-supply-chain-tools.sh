#!/bin/sh
# Installs pinned Trivy, Syft and cosign into /usr/local/bin after verifying the
# SHA-256 published with each release. Used by the GitLab publication job.
set -eu
dir=$(mktemp -d)
trap 'rm -rf "$dir"' EXIT
fetch() { # url sha256 file
  wget -q -O "$dir/$3" "$1"
  echo "$2  $dir/$3" | sha256sum -c -s || { echo "checksum mismatch for $3" >&2; exit 1; }
}
fetch https://github.com/aquasecurity/trivy/releases/download/v0.75.0/trivy_0.75.0_Linux-64bit.tar.gz \
  c6e65abddb348e25f10549df887045629cf28cc72453cd1c63acb717316b3f3f trivy.tar.gz
fetch https://github.com/anchore/syft/releases/download/v1.54.1/syft_1.54.1_linux_amd64.tar.gz \
  c069905b391cc4c20a5ba65ad5c10be2a7ba074f8ea6ad203e24d14e303dad47 syft.tar.gz
fetch https://github.com/sigstore/cosign/releases/download/v3.1.3/cosign-linux-amd64 \
  4629c757b7618056f8ddd7e2625ae9fdd94c0372a65049520bc7d9df9efc7f71 cosign
tar -xzf "$dir/trivy.tar.gz" -C /usr/local/bin trivy
tar -xzf "$dir/syft.tar.gz" -C /usr/local/bin syft
install -m 0755 "$dir/cosign" /usr/local/bin/cosign
trivy --version | head -1
syft version | grep '^Version'
cosign version 2>&1 | grep GitVersion
