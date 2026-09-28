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

All addresses and credentials are supplied by the target organization. Keep the registry internal and configure immutable tags, repository permissions and retention there. The CI script refuses an unprotected or non-default branch, rejects malformed image coordinates, and passes the registry token to `docker login` through stdin. A partial push can leave some images present under a commit tag; promotion must check that all ten exist before deployment. Protect the pipeline definition and runner because a trusted branch can modify its own CI steps.

Locally, `sh tests/ci/publish-images.sh` checks the publication guard and image commands without contacting a registry. `scripts/verify-helm.sh` runs strict schema checks in addition to the lighter CI Helm render. The [Jenkins evaluation workflow](deployment-automation.md) now defines promotion, smoke and rollback against protected namespaces. These local checks do not prove that GitLab, SonarQube, JFrog, Jenkins or a cluster is configured here. The [Kubernetes guide](kubernetes.md) describes the current chart and its security limits.
