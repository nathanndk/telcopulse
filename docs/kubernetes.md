# Kubernetes evaluation deployment

The [Helm chart](../infrastructure/helm/telcopulse/Chart.yaml) packages the ten TelcoPulse application services for a protected evaluation namespace. It references external PostgreSQL, Redis and Kafka installations; it does not install or operate those stateful systems. The chart creates ClusterIP Services and per-service ConfigMaps, a ServiceAccount without an API-token mount, an Ingress for the web console, a pre-install/upgrade migration Job, and optional HPA and PodDisruptionBudget resources for the web and gateway. Every Deployment has [startup, readiness and liveness probes](https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/) plus CPU/memory requests and limits. Only the web Service is exposed by Ingress.

**This is not a production deployment.** The current Go executables require `APP_MODE=local`, incident data scopes are limited to development and staging, authentication uses local accounts rather than SSO, and the protected gateway still uses environment-backed service tokens over internal HTTP. `values-production.yaml` is a renderable production-like sizing/availability example using the staging data scope, not approval to deploy customer-facing or sensitive workloads. A real cluster, external dependencies, identity integration, transport security, secret rotation, monitoring and disaster recovery still need validation.

## Prerequisites

Provide a namespace, DNS name, ingress controller and TLS certificate, a private image registry containing matching builds of all ten services, and reachable PostgreSQL, Redis and Kafka endpoints. Create a registry pull Secret in each namespace and pass its name through `imagePullSecrets`; the Jenkins evaluation workflow expects `telcopulse-registry-pull`. The web image must be built with `API_URL=http://api-gateway:8080`: Next.js embeds this rewrite target in the image, and the chart intentionally names the gateway Service `api-gateway`. Provide an OpenTelemetry collector and Prometheus endpoint if distributed tracing and automatic alert ingestion are required; empty endpoints disable export/ingestion rather than inventing evidence. `TRACE_VIEWER_URL` and `PROMETHEUS_VIEWER_URL` should be browser-reachable, non-secret URLs.

Before installation, create the target namespace, then an Opaque Secret named by `existingSecret` (default `telcopulse-runtime`) in that namespace with these keys:

| Key | Used by |
| --- | --- |
| `DATABASE_URL` | Gateway, all Go domain services, migration and optional bootstrap Jobs |
| `SERVICE_TOKEN` | Gateway and Go domain services |
| `INCIDENT_OPERATOR_TOKEN` | Gateway and incident service only |
| `SIMULATION_OPERATOR_TOKEN` | Gateway and simulation service only |
| `NOTIFICATION_OPERATOR_TOKEN` | Gateway and notification service only |
| `AUTH_BOOTSTRAP_PASSWORD` | Optional one-time bootstrap Job only |
| `SPLUNK_HEC_TOKEN` | Optional structured log export from every Go service |
| `SPLUNK_SEARCH_TOKEN` | Optional incident log search, incident service only |
| `DATADOG_TOKEN` | Optional infrastructure metric search, incident service only |
| `OTLP_AUTHORIZATION` | Optional authenticated OTLP export from every Go service; file contents are the full `Authorization` header value |
| `OTLP_CA_CERTIFICATE` | Optional PEM CA bundle for OTLP export from every Go service |

The four service and operator tokens must be distinct, randomly generated values of at least 32 characters. Use a secret manager or private files with `kubectl create secret generic --from-file`; do not put credentials in Helm values, command arguments, Git, or rendered manifests. The [Secret example](../infrastructure/kubernetes/runtime-secret.example.yaml) is intentionally empty. Configure the PostgreSQL URL for TLS and network access appropriate to the environment. The chart's pre-install migration hook requires the Secret to exist **before** `helm upgrade --install`.

For a fresh local-account database, set `bootstrap.enabled=true` for the initial install and include `AUTH_BOOTSTRAP_PASSWORD`. The post-install Job feeds it to the auth-service bootstrap command over stdin. Existing databases can leave bootstrap disabled. Use `AUTH_REQUIRED=true` (the chart default); the gateway and UI enforce roles but this is still local-account evaluation mode.

## Build, render and install

Build and push each Go image from `services/Dockerfile` using its `SERVICE` build argument. The gateway uses `SERVICE=api-gateway`; the other names match their service directories. Build the web image from `apps/web/Dockerfile` with `--build-arg API_URL=http://api-gateway:8080`. Tag all ten images with the same immutable release tag in the configured registry. Never deploy the example `registry.example.invalid` image path unchanged.

Run `scripts/verify-helm.sh` to lint the chart and schema-check dev, staging and production-like renderings against Kubernetes 1.34. This uses pinned Helm and kubeconform containers, Ruby's standard YAML parser, and Docker network access; it does not deploy anything. The source files also include a [namespace example](../infrastructure/kubernetes/namespace.example.yaml). An evaluation install, after the Secret and TLS certificate exist, looks like:

```sh
helm upgrade --install telcopulse infrastructure/helm/telcopulse \
  --namespace telcopulse-dev \
  -f infrastructure/helm/telcopulse/values-dev.yaml \
  --set image.registry=registry.example.invalid/telcopulse \
  --set image.tag=REPLACE_WITH_IMMUTABLE_TAG \
  --set runtime.redisURL=redis://REPLACE_WITH_REDIS_SERVICE:6379/0 \
  --set runtime.kafkaBrokers=REPLACE_WITH_KAFKA_SERVICE:9092 \
  --wait --timeout 10m
```

Replace every example address and tag with local, fictional or authorized infrastructure values. `runtime.webOrigin` must equal `https://ingress.host`; the chart rejects a mismatch because the gateway uses that origin for unsafe cookie-authenticated requests. The Ingress references an existing TLS Secret. Set `runtime.otlpEndpoint`, `runtime.prometheusURL` and browser viewer URLs for the deployed observability stack. The staging and production-like values request two web and gateway replicas with a disruption budget; production-like values also add CPU HPAs and require cluster metrics-server. Scaling the remaining workers needs separate load and event-consumer validation.

## Optional vendor evidence

The [vendor values example](../infrastructure/helm/telcopulse/values-vendors.example.yaml) enables Splunk HEC export, Splunk incident log search, authenticated OTLP/Dynatrace trace export, Jaeger trace search, and Datadog Kubernetes metric search. Its hosts are fictional. Supply real, authorized HTTPS endpoints and the matching keys in `existingSecret`, then add `-f infrastructure/helm/telcopulse/values-vendors.example.yaml` to the install command after editing a private copy of the example. Keep credential bytes out of values files: `observability.*TokenKey` and `observability.otlp.*Key` name existing Secret keys only. The chart rejects insecure or malformed vendor URLs and authenticated OTLP over HTTP.

Each credential is projected as a single key into a read-only file. HEC and OTLP credentials reach the nine Go pods; Splunk Search and Datadog credentials reach only the incident pod. The web pod receives no vendor credentials. `JAEGER_QUERY_URL` carries no credential and reaches only incident service. A reachable Jaeger query API and trace exporter are both needed for incident trace evidence; a trace viewer URL alone is just a link. The chart checks Secret key names but cannot check that the Secret exists or that a vendor accepts and indexes events. Restart the affected Deployments after rotating vendor credentials so the Go clients reload their startup configuration. The render verifier checks vendor and default manifests, Secret-key scope, invalid endpoint rejection, and Kubernetes schema; live indexing still requires an actual cluster and vendor accounts.

## OpenShift and failure exercises

The pod spec omits a fixed `runAsUser`, requests non-root execution, disables API-token automount, drops capabilities and blocks privilege escalation. The Go binary is executable without a fixed UID. The web image makes its Next.js cache group-0 writable for an OpenShift-assigned UID, following [Red Hat's arbitrary-UID image guidance](https://docs.redhat.com/en/documentation/openshift_container_platform/4.2/html/images/creating-images). The chart uses standard Kubernetes Ingress rather than an OpenShift-only Route; the target cluster must provide an ingress controller and admit the security context. Actual SCC admission and rollout behavior are **not yet verified on an OpenShift cluster**.

Startup/readiness/liveness probes and resource limits allow controlled exercises of failed readiness, crash loops and OOM conditions. An HPA responds to CPU pressure only when a metrics server is present. Do not equate a valid manifest with proof of those failure scenarios; run them in an isolated evaluation namespace, observe pod events and TelcoPulse business telemetry, then restore the workload. `kubectl` and Helm rendering alone cannot prove real cluster behavior.
