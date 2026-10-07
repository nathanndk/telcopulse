# CI and artifact flow

The [GitLab pipeline](../.gitlab-ci.yml) is an executable starting point for the build side of TelcoPulse's enterprise delivery flow. Merge requests and default-branch commits run frontend lint, typecheck and production build; Go vet and race-enabled tests; a second Go run against a disposable PostgreSQL service with migrations; Helm strict lint and profile rendering; and publication-guard tests. The PostgreSQL CI service uses trust authentication only on its isolated ephemeral runner network and never contains customer data. Kafka and full browser tests still need dedicated service and application fixtures; the Go Kafka tests skip when `TEST_KAFKA_BROKER` is absent.

On a **protected default branch**, SonarScanner sends source and Go coverage to SonarQube and waits for the server's quality gate. Failure blocks the image job. Only after that gate passes does the Docker-in-Docker job build and push the nine Go images and web image to a JFrog-compatible Docker registry. Every image receives the full commit SHA tag. The Helm chart is packaged as a short-lived GitLab artifact; it is not yet published to the registry. No deployment is triggered by this pipeline.

Set up a protected GitLab default branch, required merge-request approvals and successful-pipeline merge checks. Use a trusted, isolated runner with a privileged Docker executor **only for the publication job**; runner selection and network policy must enforce that boundary. Configure a SonarQube project with key `telcopulse`, a reachable server, and an appropriate quality gate. The server's gate policy is authoritative; this repository does not invent a passing threshold.

Configure these GitLab CI variables, masked and protected where applicable:

| Variable | Purpose |
| --- | --- |
| `SONAR_HOST_URL` | Internal SonarQube server URL, available only to the protected branch job |
| `SONAR_TOKEN` | SonarQube analysis token; masked and protected |
| `JFROG_REGISTRY` | Registry host with optional port, without scheme or path |
| `JFROG_IMAGE_PATH` | Lowercase Docker repository path, such as `telcopulse` |
| `JFROG_USERNAME` | Registry publisher identity |
| `JFROG_PASSWORD` | Registry publisher token; masked and protected |
| `COSIGN_PRIVATE_KEY` | cosign signing key (encrypted PEM from `cosign generate-key-pair`); masked and protected |
| `COSIGN_PASSWORD` | Passphrase of that key; masked and protected |

All addresses and credentials are supplied by the target organization. Keep the registry internal and configure immutable tags, repository permissions and retention there. The CI script refuses an unprotected or non-default branch, rejects malformed image coordinates, and passes the registry token to `docker login` through stdin. A partial push can leave some images present under a commit tag; promotion must check that all ten exist before deployment. Protect the pipeline definition and runner because a trusted branch can modify its own CI steps.

The same cosign 3.1.3 commands were exercised against a local registry. A payment-service image was signed and given a Syft SBOM attestation without the transparency log. `cosign verify` and `verify-attestation` succeeded with the matching public key and failed with another key. Locally, `sh tests/ci/publish-images.sh` checks the publication guard and image commands without contacting a registry. `scripts/verify-helm.sh` runs strict schema checks in addition to the lighter CI Helm render. The [Jenkins evaluation workflow](deployment-automation.md) now defines promotion, smoke and rollback against protected namespaces. These local checks do not prove that GitLab, SonarQube, JFrog, Jenkins or a cluster is configured here. The [Kubernetes guide](kubernetes.md) describes the current chart and its security limits.

## Supply-chain controls

- **Dependency and IaC scanning** on every merge request: `dependency-scan` runs `trivy fs` over `services/go.mod` and `package-lock.json` and fails on fixable HIGH/CRITICAL vulnerabilities. `iac-scan` runs `trivy config` over the Dockerfiles, Helm chart, Kubernetes manifests and OpenTofu, and fails on HIGH/CRITICAL misconfigurations. Accepted exceptions live in [`.trivyignore.yaml`](../.trivyignore.yaml), each with a written reason: Alloy's Docker socket access, the backup job's root-then-drop entrypoint, and the evaluation Kafka's writable root filesystem.
- **Image gate:** `scripts/publish-images.sh` scans each built image with Trivy **before** pushing it. A fixable HIGH or CRITICAL vulnerability stops the job, and that image never reaches the registry.
- **SBOM and signatures:** after each push, Syft writes a CycloneDX SBOM (`sbom/<service>.cdx.json`). cosign signs the pushed **digest**, not the mutable tag, and attaches the SBOM as a signed `cyclonedx` attestation. cosign 3 uploads to the public Rekor transparency log by default, which would publish private image digests. The job therefore signs without the log unless `COSIGN_TRANSPARENCY_LOG=public`, and the Jenkins verification skips the log check to match. Use a private Rekor instance if transparency is required. `release-digests.txt` lists the ten digests. Both are kept as job artifacts for 90 days.
- **Pinned tools:** `scripts/install-supply-chain-tools.sh` installs Trivy 0.75.0, Syft 1.54.1 and cosign 3.1.3 only after checking each release's published SHA-256.
- **Deploy-time verification:** the Jenkins preflight runs `cosign verify` and `cosign verify-attestation --type cyclonedx` for all ten images with the public key in the `telcopulse-cosign-public-key` credential. STAGING and PRODUCTION_LIKE refuse to deploy without that key, and one unsigned image fails the whole release. A cluster admission policy (for example the Sigstore policy-controller or Kyverno `verifyImages`) would also stop manual `kubectl` deployments; it is not configured here.

Remediation on 2026-10-07: the first Trivy scan found CRITICAL findings in `google.golang.org/grpc` 1.75.0, `github.com/jackc/pgx/v5` 5.7.6 and npm's bundled `tar`, plus HIGH findings in `golang.org/x/{crypto,net,text}`, OpenTelemetry, `sharp` and `source-map-js`. Go modules were upgraded, which requires Go 1.26 for builds. `sharp` and `source-map-js` were updated within their semver ranges. The web runtime image no longer ships npm, npx, corepack or yarn, because the standalone server needs only `node`. All ten images and both lockfiles then scanned clean. The same cosign 3.1.3 commands were exercised against a local registry. A payment-service image was signed and given a Syft SBOM attestation without the transparency log. `cosign verify` and `verify-attestation` succeeded with the matching public key and failed with another key. Locally, `sh tests/ci/publish-images.sh` and `bash tests/ci/promote-release.sh` cover the scan-before-push ordering, the vulnerable-image refusal, signing and attestation, and signature enforcement, with mocked tools.

