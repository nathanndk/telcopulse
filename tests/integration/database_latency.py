"""Verify a real 4.2-second PostgreSQL span and recovery after stopping the run."""
import json
import os
import time
import urllib.request
import uuid

BASE = os.environ.get("WEB_URL", "http://localhost:3001")


def call(path, data=None, key=None):
    headers = {"Content-Type": "application/json"}
    if key:
        headers["Idempotency-Key"] = key
    req = urllib.request.Request(BASE + "/api/v1" + path, data=None if data is None else json.dumps(data).encode(), headers=headers)
    with urllib.request.urlopen(req, timeout=20) as response:
        return response.status, json.load(response)


command = {"scenario": "database-latency", "environment": "staging", "percentage": 100, "duration_seconds": 60, "delay_ms": 4200, "reason": "Verify measured database latency and trace correlation"}
status, run = call("/simulations", command, str(uuid.uuid4()))
assert status == 201
payload = {"customer_id": "cus-001", "package_id": "pkg-10", "payment_method": "E-Wallet", "environment": "staging"}
key = str(uuid.uuid4())
try:
    status, slow = call("/transactions", payload, key)
    assert status == 201 and slow["status"] == "SUCCESS" and slow["duration_ms"] >= 4100, slow
    status, replay = call("/transactions", payload, key)
    assert status == 200 and replay["id"] == slow["id"]
    _, detail = call("/simulations/" + run["id"])
    assert detail["observed"] == 1 and detail["selected"] == 1, detail
    deadline = time.monotonic() + 15
    found = []
    while time.monotonic() < deadline:
        with urllib.request.urlopen("http://localhost:16686/api/traces/" + slow["trace_id"], timeout=10) as response:
            traces = json.load(response)
        found = [span for trace in traces.get("data", []) for span in trace.get("spans", []) if trace["processes"][span["processID"]]["serviceName"] == "payment-service" and span["operationName"] == "postgresql SELECT" and span["duration"] >= 4100000]
        if found:
            break
        time.sleep(1)
    assert found, "No measured 4.2-second payment PostgreSQL span"
finally:
    _, stopped = call("/simulations/" + run["id"] + "/stop", {"reason": "Restore ordinary latency after verification"})
    assert not stopped["active"]
_, recovered = call("/transactions", payload, str(uuid.uuid4()))
assert recovered["status"] == "SUCCESS" and recovered["duration_ms"] < 2000, recovered
print(json.dumps({"run_id": run["id"], "slow_transaction": slow["id"], "duration_ms": slow["duration_ms"], "database_span_us": found[0]["duration"], "recovered_ms": recovered["duration_ms"]}))
