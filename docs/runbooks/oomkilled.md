# OOMKilled

**Scope.** OOMKilled is a container termination reason in a real isolated Kubernetes/OpenShift evaluation namespace; the local Go process memory chart is not proof of a kernel kill. No dedicated TelcoPulse OOM injector or pod-restart metric is deployed yet.

**Triage.** Capture the pod/container's `lastState.terminated.reason`, exit code, restart count, memory limit, node conditions and restart timestamps with `kubectl describe pod POD` and Events. Compare container memory working set and limit in the cluster's metrics platform, plus per-process Go memory and transaction outcomes. A growing process memory graph alone does not establish OOMKilled. Inspect recent image/config changes and any workload spike.

**Differentiate.** Memory leak, undersized limit, burst load, co-located node pressure and startup allocation spikes need different remedies. Correlate the precise restart interval with payment or other service errors; a graceful process restart and a failed readiness probe are separate events. If no Kubernetes memory source is configured, state that infrastructure evidence is unavailable.

**Mitigate.** In a controlled evaluation, stop the fault workload if one was explicitly started. Roll back a verified regressing release or use a reviewed capacity change only after identifying the pressure source. Do not merely increase limits without confirming node headroom and the allocation pattern. Keep business traffic and rollback risks visible to the Incident Commander.

**Recover and close.** Verify stable memory relative to the configured limit, no new OOM terminations, expected ready replicas, successful fresh purchases, and any pending workflow/notification recovery. Include the terminated container's evidence and a memory-test or code-fix action in the postmortem. This guide has not been validated against a live cluster.
