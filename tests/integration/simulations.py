"""Exercise a bounded synthetic decline through the public API, always stopping it."""
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
    request = urllib.request.Request(BASE + "/api/v1" + path, data=None if data is None else json.dumps(data).encode(), headers=headers)
    with urllib.request.urlopen(request, timeout=15) as response:
        return response.status, json.load(response)


def purchase(environment="staging", key=None):
    payload = {"customer_id": "cus-001", "package_id": "pkg-10", "payment_method": "E-Wallet", "environment": environment}
    status, result = call("/transactions", payload, key or str(uuid.uuid4()))
    deadline = time.monotonic() + 20
    while result["status"] == "PROCESSING" and time.monotonic() < deadline:
        time.sleep(1)
        _, result = call("/transactions/" + result["id"])
    return status, result


creation_key = str(uuid.uuid4())
command = {"environment": "staging", "scenario": "payment-decline", "percentage": 100, "duration_seconds": 180, "reason": "Verify controlled synthetic business failure and mitigation"}
status, run = call("/simulations", command, creation_key)
assert status == 201 and run["active"], run
try:
    status, replay = call("/simulations", command, creation_key)
    assert status == 200 and replay["id"] == run["id"]
    purchase_key = str(uuid.uuid4())
    status, failed = purchase(key=purchase_key)
    assert status in (200, 201, 202) and failed["status"] == "FAILED", failed
    assert failed["error_code"] == "SIMULATED_PAYMENT_DECLINED", failed
    assert failed["trace_id"], failed
    for _ in range(4):
        _, result = purchase()
        assert result["error_code"] == "SIMULATED_PAYMENT_DECLINED", result
    _, isolated = purchase("development")
    assert isolated["status"] == "SUCCESS", isolated
finally:
    _, stopped = call("/simulations/" + run["id"] + "/stop", {"reason": "Verification mitigation; restore new purchases"})
    assert not stopped["active"] and stopped["stopped_at"]
_, recovered = purchase()
assert recovered["status"] == "SUCCESS", recovered
_, replay = purchase(key=purchase_key)
assert replay["id"] == failed["id"] and replay["error_code"] == "SIMULATED_PAYMENT_DECLINED", replay
print(json.dumps({"run_id": run["id"], "failed_transaction": failed["id"], "recovered_transaction": recovered["id"], "verified": ["exact creation replay", "five real business failures", "environment isolation", "explicit stop", "new transaction recovery", "stable failed retry"]}))
