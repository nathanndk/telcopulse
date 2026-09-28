# Pod CrashLoopBackOff

**Scope.** This requires a real isolated Kubernetes/OpenShift evaluation namespace; Compose has no pod restart state. The [Helm chart](../kubernetes.md) defines startup/readiness/liveness probes and resource limits, but chart rendering does not prove this failure scenario. Use the namespace and context approved for the exercise.

**Triage.** Record namespace, workload, image digest, replica count and first restart time. Inspect `kubectl --context "$KUBE_CONTEXT" -n "$NAMESPACE" get pods -o wide`, `describe pod POD`, previous-container logs and recent Events. Check exit code, probe failures, image pull errors, missing Secret/ConfigMap keys and dependency connectivity. Compare the service's Prometheus `up`, available replicas, HTTP errors and business transactions. If the reason is OOMKilled, follow [OOMKilled](oomkilled.md).

**Mitigate.** If the new release is the cause, use the reviewed [deployment rollback](bad-deployment.md) or restore the previously verified configuration. If an external dependency is down, restore it before restarting pods repeatedly. Avoid deleting all pods or changing probes/limits to hide the failure. Keep at least one working replica when available and document any customer-impacting action.

**Recover and close.** Verify new pods pass startup/readiness, the expected replica count stays Available without new restarts through a meaningful observation window, and a fresh synthetic purchase and notification complete. Confirm alert clearance and record the pod Events and exact failed condition in the RCA. No live cluster exercise has yet verified this guide.
