"""Kubernetes game day on the kind evaluation cluster (scripts/kind-evaluation.sh).

Each experiment states a hypothesis, runs steady synthetic purchases from a k6 Job
inside the cluster, injects one failure, and judges the hypothesis from purchase
outcomes and Prometheus. Every injection is removed in a finally block.

    python3 tests/kubernetes/game_day.py [baseline pod-kill network-delay kafka-partition node-drain bad-canary]

Prints one JSON line per experiment. Evaluation cluster only.
"""
import json
import os
import subprocess
import sys
import time
import urllib.parse
import urllib.request

CONTEXT = "kind-telcopulse"
NS = "telcopulse"
REPO = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
PROMETHEUS = "http://127.0.0.1:19090"


def kubectl(*args, check=True, stdin=None):
    return subprocess.run(["kubectl", "--context", CONTEXT, *args], check=check, capture_output=True, text=True, input=stdin).stdout


def promql(expression):
    with urllib.request.urlopen(PROMETHEUS + "/api/v1/query?" + urllib.parse.urlencode({"query": expression}), timeout=10) as response:
        return json.load(response)["data"]["result"]


def firing(alert):
    return [r["metric"] for r in promql(f'ALERTS{{alertstate="firing",alertname="{alert}"}}')]


def wait(predicate, seconds, message):
    end = time.monotonic() + seconds
    while time.monotonic() < end:
        value = predicate()
        if value:
            return value
        time.sleep(5)
    raise AssertionError(message)


def start_load(name, rate, duration):
    kubectl("-n", "telcopulse-load", "create", "configmap", "kind-load", f"--from-file=kind-load.js={REPO}/tests/load/kind-load.js", "--dry-run=client", "-o", "yaml")
    manifest = kubectl("-n", "telcopulse-load", "create", "configmap", "kind-load", f"--from-file=kind-load.js={REPO}/tests/load/kind-load.js", "--dry-run=client", "-o", "yaml")
    kubectl("apply", "-f", "-", stdin=manifest)
    kubectl("-n", "telcopulse-load", "delete", "job", name, "--ignore-not-found", "--wait=true")
    job = {
        "apiVersion": "batch/v1", "kind": "Job",
        "metadata": {"name": name, "namespace": "telcopulse-load"},
        "spec": {"backoffLimit": 0, "template": {"spec": {
            "restartPolicy": "Never",
            "containers": [{
                "name": "k6", "image": "grafana/k6:2.2.0", "args": ["run", "--quiet", "/scripts/kind-load.js"],
                "env": [{"name": "RATE", "value": str(rate)}, {"name": "DURATION_SECONDS", "value": str(duration)},
                        {"name": "ADMIN_PASSWORD", "valueFrom": {"secretKeyRef": {"name": "load-credentials", "key": "ADMIN_PASSWORD"}}}],
                "volumeMounts": [{"name": "scripts", "mountPath": "/scripts"}],
                "resources": {"requests": {"cpu": "50m", "memory": "64Mi"}},
            }],
            "volumes": [{"name": "scripts", "configMap": {"name": "kind-load"}}],
        }}},
    }
    kubectl("apply", "-f", "-", stdin=json.dumps(job))


def finish_load(name, timeout=600):
    kubectl("-n", "telcopulse-load", "wait", "--for=condition=complete", f"job/{name}", f"--timeout={timeout}s")
    metrics = json.loads(kubectl("-n", "telcopulse-load", "logs", f"job/{name}").strip().splitlines()[-1])
    value = lambda metric, field="count": metrics.get(metric, {}).get("values", {}).get(field, 0)
    return {
        "success": int(value("purchase_success")), "technical_failures": int(value("purchase_technical_failure")),
        "http_errors": int(value("purchase_http_error")), "unresolved": int(value("purchase_unresolved")),
        "p95_ms": round(value("purchase_end_to_end_ms", "p(95)")), "p99_ms": round(value("purchase_end_to_end_ms", "p(99)")),
        "dropped": int(value("dropped_iterations")),
    }


def chaos(file):
    return kubectl("apply", "-f", f"{REPO}/infrastructure/chaos/{file}")


def unchaos(file):
    kubectl("delete", "-f", f"{REPO}/infrastructure/chaos/{file}", "--ignore-not-found", check=False)


def report(name, hypothesis, result, held):
    print(json.dumps({"experiment": name, "hypothesis": hypothesis, "held": held, **result}), flush=True)
    return held


def baseline():
    start_load("baseline", 3, 90)
    result = finish_load("baseline")
    return report("baseline", "Steady 3 purchases/s complete without failures; p95 below 2 s.", result,
                  result["technical_failures"] == 0 and result["unresolved"] == 0 and result["http_errors"] == 0 and result["p95_ms"] < 2000)


def pod_kill():
    start_load("pod-kill", 3, 120)
    time.sleep(40)
    before = kubectl("-n", NS, "get", "pods", "-l", "app.kubernetes.io/name=payment-service", "-o", "jsonpath={.items[*].metadata.name}")
    try:
        chaos("pod-kill-payment.yaml")
        wait(lambda: kubectl("-n", NS, "get", "pods", "-l", "app.kubernetes.io/name=payment-service", "-o", "jsonpath={.items[*].metadata.name}") != before, 60, "payment pod was not replaced")
    finally:
        unchaos("pod-kill-payment.yaml")
    result = finish_load("pod-kill")
    result["killed_pod_replaced"] = True
    return report("pod-kill", "Killing one of two payment pods causes no technical failures or stuck purchases.", result,
                  result["technical_failures"] == 0 and result["unresolved"] == 0)


def network_delay():
    start_load("network-delay", 3, 240)
    time.sleep(30)
    try:
        chaos("network-delay-postgres.yaml")
        fired = None
        try:
            fired = wait(lambda: firing("PurchaseLatencyBudgetBurn"), 200, "latency burn alert did not fire")
        except AssertionError:
            pass
    finally:
        unchaos("network-delay-postgres.yaml")
    result = finish_load("network-delay")
    result["latency_burn_alert_fired"] = bool(fired)
    incidents = promql('sum(increase(http_requests_total{service="incident-service",route="POST /internal/incidents"}[10m]))')
    return report("network-delay", "400 ms PostgreSQL latency from payment slows purchases and fires PurchaseLatencyBudgetBurn without technical failures.", result,
                  result["technical_failures"] == 0 and bool(fired))


def kafka_partition():
    start_load("kafka-partition", 3, 180)
    time.sleep(30)
    try:
        chaos("partition-kafka.yaml")
        lag = wait(lambda: [r for r in promql('sum(kafka_consumer_lag{namespace="telcopulse"})') if float(r["value"][1]) > 0] or
                   [r for r in promql('max(event_outbox_pending{namespace="telcopulse"})') if float(r["value"][1]) > 0], 120,
                   "partition produced no backlog")
        time.sleep(60)
    finally:
        unchaos("partition-kafka.yaml")
    result = finish_load("kafka-partition")
    drained = wait(lambda: all(float(r["value"][1]) == 0 for r in promql('max(event_outbox_pending{namespace="telcopulse"}) or vector(0)')), 300, "outbox did not drain after the partition healed")
    result["backlog_observed"] = bool(lag)
    result["outbox_drained_after_heal"] = bool(drained)
    return report("kafka-partition", "Partitioning notification-service from Kafka leaves purchases unaffected; the backlog drains after healing.", result,
                  result["technical_failures"] == 0 and result["unresolved"] == 0 and result["http_errors"] == 0)


def node_drain():
    pods = json.loads(kubectl("-n", NS, "get", "pods", "-l", "app.kubernetes.io/name=api-gateway", "-o", "json"))["items"]
    node = pods[0]["spec"]["nodeName"]
    start_load("node-drain", 3, 150)
    time.sleep(30)
    try:
        kubectl("drain", node, "--ignore-daemonsets", "--delete-emptydir-data", "--timeout=240s")
        drained_at = time.monotonic()
    finally:
        time.sleep(20)
        kubectl("uncordon", node, check=False)
    result = finish_load("node-drain")
    result["drained_node"] = node
    return report("node-drain", "Draining a worker that hosts the gateway respects the PodDisruptionBudgets and loses no purchases.", result,
                  result["technical_failures"] == 0 and result["unresolved"] == 0 and result["http_errors"] == 0)


def bad_canary():
    """A release that points the gateway at an unreachable Redis returns 503 for
    every purchase; the canary analysis must abort it and keep the stable revision."""
    stable = kubectl("-n", NS, "get", "rollout", "api-gateway", "-o", "jsonpath={.status.stableRS}")
    start_load("bad-canary", 4, 300)
    time.sleep(20)
    patch = json.dumps({"spec": {"source": {"helm": {"parameters": [{"name": "runtime.redisURL", "value": "redis://redis-unreachable.telcopulse-deps.svc:6379/0"}]}}}})
    started = time.monotonic()
    try:
        kubectl("-n", "argocd", "patch", "application", "telcopulse-kind", "--type", "merge", "-p", patch)
        phase = wait(lambda: kubectl("-n", NS, "get", "rollout", "api-gateway", "-o", "jsonpath={.status.phase}") == "Degraded" and "Degraded", 420, "canary was not aborted")
        aborted_after = round(time.monotonic() - started)
        message = kubectl("-n", NS, "get", "rollout", "api-gateway", "-o", "jsonpath={.status.message}")
        stable_after = kubectl("-n", NS, "get", "rollout", "api-gateway", "-o", "jsonpath={.status.stableRS}")
    finally:
        kubectl("-n", "argocd", "patch", "application", "telcopulse-kind", "--type", "json", "-p", '[{"op":"remove","path":"/spec/source/helm/parameters"}]', check=False)
    wait(lambda: kubectl("-n", NS, "get", "rollout", "api-gateway", "-o", "jsonpath={.status.phase}") == "Healthy", 420, "rollout did not return to Healthy after the revert")
    result = finish_load("bad-canary")
    result.update({"aborted_after_s": aborted_after, "abort_message": message[:200], "stable_kept": stable == stable_after})
    return report("bad-canary", "Analysis of canary pods aborts a release that fails purchases and keeps the stable revision serving.", result,
                  phase == "Degraded" and stable == stable_after)


EXPERIMENTS = {"baseline": baseline, "pod-kill": pod_kill, "network-delay": network_delay, "kafka-partition": kafka_partition, "node-drain": node_drain, "bad-canary": bad_canary}

forward = subprocess.Popen(["kubectl", "--context", CONTEXT, "-n", "monitoring", "port-forward", "svc/kube-prometheus-stack-prometheus", "19090:9090"], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
try:
    wait(lambda: promql("up"), 30, "Prometheus port-forward failed")
    names = sys.argv[1:] or list(EXPERIMENTS)
    results = {name: EXPERIMENTS[name]() for name in names}
finally:
    forward.terminate()
sys.exit(0 if all(results.values()) else 1)
