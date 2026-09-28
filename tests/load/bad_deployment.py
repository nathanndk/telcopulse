"""Run bounded k6 degradation and recovery against local Compose only."""

import json
import os
from pathlib import Path
import subprocess
import time
import urllib.parse
import urllib.request
import uuid


ROOT = Path(__file__).resolve().parents[2]
WEB_URL = os.environ.get("WEB_URL", "http://localhost:3001").rstrip("/")
if WEB_URL not in {"http://localhost:3000", "http://localhost:3001", "http://127.0.0.1:3000", "http://127.0.0.1:3001"}:
    raise SystemExit("This load demonstration only targets the local Compose stack")


def request(path, data=None, key=None):
    headers = {"Content-Type": "application/json"}
    if key:
        headers["Idempotency-Key"] = key
    req = urllib.request.Request(
        WEB_URL + "/api/v1" + path,
        data=None if data is None else json.dumps(data).encode(), headers=headers,
    )
    with urllib.request.urlopen(req, timeout=15) as response:
        return json.load(response)


def metric(status):
    query = 'sum(transaction_total{environment="staging",status="' + status + '"})'
    url = "http://localhost:9090/api/v1/query?" + urllib.parse.urlencode({"query": query})
    with urllib.request.urlopen(url, timeout=5) as response:
        result = json.load(response)["data"]["result"]
    return float(result[0]["value"][1]) if result else 0.0


def wait_for(label, check, seconds=30):
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        value = check()
        if value:
            return value
        time.sleep(1)
    raise AssertionError(label + " did not become ready")


def load(expectation, duration):
    command = [
        "docker", "run", "--rm", "--network", "telcopulse_frontend", "-i",
        "-e", "EXPECT=" + expectation,
        "-e", "RATE=1", "-e", "DURATION_SECONDS=" + str(duration),
        "grafana/k6:2.2.0", "run", "-",
    ]
    script = (ROOT / "tests/load/purchases.js").read_text()
    result = subprocess.run(command, input=script, text=True, capture_output=True, timeout=duration + 60, cwd=ROOT)
    if result.returncode != 0:
        raise AssertionError("k6 " + expectation + " run failed:\n" + result.stdout[-4000:] + result.stderr[-2000:])
    lines = [line.strip() for line in result.stdout.splitlines() if any(name in line for name in (
        "business_target_outcome_rate", "business_failure_rate", "business_terminal_outcomes",
        "business_unresolved_outcomes", "http_req_failed", "dropped_iterations",
    ))]
    return lines[-6:]


failure_before = metric("FAILED")
run = request("/simulations", {
    "environment": "staging", "scenario": "bad-deployment", "percentage": 100,
    "duration_seconds": 150, "reason": "Bounded k6 business degradation and rollback verification",
}, str(uuid.uuid4()))
try:
    wait_for("deployment marker", lambda: request("/simulations/" + run["id"])["deployment_report"] == "Completed")
    degraded = load("degraded", 15)
    wait_for("failed business metric", lambda: metric("FAILED") > failure_before, 30)
finally:
    request("/simulations/" + run["id"] + "/stop", {"reason": "Restore business traffic after bounded k6 test"})

wait_for("rollback marker", lambda: request("/deployments/" + run["deployment_id"])["status"] == "Rolled Back")
recovered = load("healthy", 10)
print(json.dumps({
    "run_id": run["id"], "deployment_id": run["deployment_id"],
    "prometheus_failed_before": failure_before, "prometheus_failed_after": metric("FAILED"),
    "degraded_k6": degraded, "recovered_k6": recovered,
}))
