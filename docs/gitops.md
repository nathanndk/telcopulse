# GitOps and progressive delivery

Git is the source of truth for what runs in each Kubernetes environment. [Argo CD](https://argo-cd.readthedocs.io/) reconciles the Helm chart from Git, and [Argo Rollouts](https://argoproj.github.io/rollouts/) replaces the gateway's rolling update with a canary that Prometheus analysis can abort. The Jenkins workflow ([deployment automation](deployment-automation.md)) remains the imperative path for clusters without Argo CD; both paths run the same chart and the same error budget policy.

## Flow

```
merge request ──► GitLab CI: tests, Trivy, SBOM, signed images (ci-cd.md)
       │
       ▼ reviewed change to image.tag / values-<env>.yaml on main
Argo CD (automated sync, prune, self-heal) ──► Helm render ──► cluster
       │
       ▼ gateway pod template changed
Argo Rollouts canary: 50% of pods ─► AnalysisRun (Prometheus) ─► 100%  or  abort
```

- **Promote:** change the image tag (or values) for an environment in a merge request. After review and merge, Argo CD syncs within its polling interval. Pre-sync hooks run migrations (the chart's Helm hooks become Argo CD PreSync/PostSync hooks).
- **Roll back:** revert the commit. Argo CD returns the environment to the previous desired state, and Argo Rollouts treats it as a new rollout of the old template. An aborted canary needs no Git change to stop the damage, because the stable ReplicaSet never stopped serving. Revert the commit so that Git matches what should run.
- **Drift:** `selfHeal: true` reverts manual changes made with `kubectl`. Emergency changes therefore go through Git too, or through an explicit Application parameter override that is visible in Argo CD and removed afterwards (as the bad-canary game day does).
- **Error budget:** the [policy](error-budget-policy.md) applies unchanged. A merge request that promotes to staging or production-like while the gate would block must carry the override reason in its description, and the canary analysis independently refuses to promote while an SLO burn alert fires.

Application manifests are in [`infrastructure/gitops`](../infrastructure/gitops): an `AppProject` that restricts source repositories, destination namespaces and resource kinds; `telcopulse-staging` for a real cluster tracking `main` on GitHub; and `telcopulse-kind` for the local evaluation. The Applications ignore `Deployment.spec.replicas`, because Argo Rollouts scales Deployments it references to zero and the HPA owns replica counts elsewhere.

## Canary analysis

With `rollouts.enabled`, the chart renders a `Rollout` for each listed service (default `api-gateway`) that references the existing Deployment through `workloadRef`. The pod template is not duplicated. The Deployment omits `replicas` so that Helm and Argo CD do not fight the controller. Each step in `rollouts.weights` (default `[50]`) is followed by an analysis using the `telcopulse-canary` AnalysisTemplate, which queries Prometheus four times, 30 s apart:

| Metric | Query scope | Fails when |
| --- | --- | --- |
| `canary-http-5xx-ratio` | `http_requests_total` of the canary pods (`rollouts_pod_template_hash` of the new ReplicaSet) | more than 5% of canary responses are 5xx |
| `canary-purchase-technical-failure-ratio` | `transaction_total` outcomes recorded by canary gateway pods | more than 5% technical failures |
| `slo-burn-alerts` | `ALERTS` for the three SLO burn alerts | any is firing |

One failed measurement aborts the rollout: the canary scales to zero and the Rollout reports `Degraded` with the failed analysis in its message. An empty or NaN result (no canary traffic) counts as success, so the analysis needs steady traffic. The kind game day runs a load generator during every rollout. The ServiceMonitor copies the `rollouts-pod-template-hash` pod label onto every series so the analysis can select canary pods. Without a traffic router, the weight is approximated by pod count: two gateway replicas give a 50% step.

The Argo Rollouts kubectl plugin shows progress (`kubectl argo rollouts get rollout api-gateway -n telcopulse --watch`). `kubectl argo rollouts abort` and `promote` are the manual overrides.

## Local evaluation

`scripts/kind-evaluation.sh deploy` serves the current checkout through a `git daemon` container on the kind Docker network (`git://telcopulse-git/repo`), so Argo CD deploys the committed branch under test without pushing it to GitHub. Uncommitted changes are invisible to Argo CD: commit before deploying. `DEPLOY_WITH=helm` installs the same chart directly for faster iteration. Results are recorded in [VERIFICATION.md](VERIFICATION.md) and the [Kubernetes game day](game-days/2026-10-07-kubernetes.md).
