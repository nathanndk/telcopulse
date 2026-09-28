"""Verify actual PostgreSQL cancellation, stable DB_TIMEOUT and recovery."""
import json
import os
import time
import urllib.request
import uuid

BASE = os.environ.get("WEB_URL", "http://localhost:3001")


def call(path, data=None, key=None):
    req = urllib.request.Request(BASE + "/api/v1" + path, data=None if data is None else json.dumps(data).encode(), headers={"Content-Type": "application/json", "Idempotency-Key": key or str(uuid.uuid4())})
    with urllib.request.urlopen(req, timeout=20) as response:
        return json.load(response)


run = call("/simulations", {"scenario": "database-timeout", "environment": "staging", "percentage": 100, "duration_seconds": 90, "delay_ms": 500, "reason": "Verify real PostgreSQL statement timeout evidence"})
payload = {"customer_id": "cus-001", "package_id": "pkg-10", "payment_method": "E-Wallet", "environment": "staging"}
key = str(uuid.uuid4())
try:
    failed = call("/transactions", payload, key)
    assert failed["status"] == "FAILED" and failed["error_code"] == "DB_TIMEOUT" and failed["duration_ms"] >= 450, failed
    for _ in range(4):
        other = call("/transactions", payload)
        assert other["error_code"] == "DB_TIMEOUT", other
    deadline = time.monotonic() + 15
    found = []
    while time.monotonic() < deadline:
        with urllib.request.urlopen("http://localhost:16686/api/traces/" + failed["trace_id"], timeout=10) as response:
            traces = json.load(response)
        found = [span for trace in traces.get("data", []) for span in trace.get("spans", []) if trace["processes"][span["processID"]]["serviceName"] == "payment-service" and span["operationName"] == "postgresql SELECT" and span["duration"] >= 450000 and any(tag["key"] == "error" and tag["value"] is True for tag in span.get("tags", []))]
        if found:
            break
        time.sleep(1)
    assert found, "Missing failed PostgreSQL span"
finally:
    stopped = call("/simulations/" + run["id"] + "/stop", {"reason": "Restore normal database operations"})
    assert not stopped["active"]
replay = call("/transactions", payload, key)
assert replay["id"] == failed["id"] and replay["error_code"] == "DB_TIMEOUT", replay
recovered = call("/transactions", payload)
assert recovered["status"] == "SUCCESS", recovered
print(json.dumps({"run_id": run["id"], "failed_transaction": failed["id"], "trace_id": failed["trace_id"], "failed_database_span_us": found[0]["duration"], "recovered_transaction": recovered["id"]}))
