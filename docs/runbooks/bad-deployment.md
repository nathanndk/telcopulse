# Bad deployment

**Trigger and scope.** A recent deployment event near a degradation is a lead, not proof. The local `bad-deployment` scenario reports a synthetic payment version `1.4.0-sim-bad` and selected failures, then a Rolled Back event on stop/expiry. It does not replace an image or Kubernetes workload. The [Jenkins evaluation workflow](../deployment-automation.md) can mutate an isolated Helm release, but has not been run against a live cluster.

**Triage.** Compare first failures and alert `activeAt` with Deployment event times in the same environment. Open the deployment detail for commit, version, reporter and status history. Inspect affected transactions for `BAD_DEPLOYMENT_PAYMENT_FAILURE`, trace/log correlation, and a simulation decision tied to the deployment ID. Check whether unaffected services or environments remain healthy; time adjacency alone cannot establish causation.

**Mitigate locally.** Stop the verified simulation in Customer Simulator. Its deployment outbox will report Rolled Back after the completion marker; verify that event reaches deployment-service before treating the marker as updated. This is a controlled data-path recovery, not a real image rollback.

**Mitigate in evaluation Kubernetes.** Use the reviewed `ROLLBACK` action with a known good Helm revision, explicit context and namespace, then allow the workflow's availability and protected web/gateway smoke checks to complete. If smoke fails, it attempts restoration of the prior revision. Helm does not reverse PostgreSQL migrations or business data; confirm schema compatibility before the rollback. Do not run the script against a customer-facing namespace.

**Recover and close.** Observe new successful transactions, business success and latency, stable deployments and notification completion, then check that no fresh failures match the regressed version. Record the exact image digest/commit and rollback revision, who approved it, the event timeline and action items for rollout guards. Synthetic deployment markers alone do not prove a real rollout or recovery.
