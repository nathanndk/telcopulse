"""Verify a synthetic bad release, durable markers, business impact and rollback."""

import json
import os
import time
import urllib.request
import uuid

BASE = os.environ.get("WEB_URL", "http://localhost:3001") + "/api/v1"


def call(path, data=None, key=None):
    headers = {"Content-Type": "application/json"}
    if key:
        headers["Idempotency-Key"] = key
    request = urllib.request.Request(
        BASE + path,
        data=None if data is None else json.dumps(data).encode(),
        headers=headers,
    )
    with urllib.request.urlopen(request, timeout=15) as response:
        return response.status, json.load(response)


def purchase(environment="staging", key=None):
    payload = {
        "customer_id": "cus-001", "package_id": "pkg-10",
        "payment_method": "E-Wallet", "environment": environment,
    }
    status, result = call("/transactions", payload, key or str(uuid.uuid4()))
    deadline = time.monotonic() + 20
    while result["status"] == "PROCESSING" and time.monotonic() < deadline:
        time.sleep(0.5)
        _, result = call("/transactions/" + result["id"])
    return status, result


def until(check, seconds=30):
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        result = check()
        if result:
            return result
        time.sleep(1)
    raise AssertionError("expected state did not arrive within " + str(seconds) + " seconds")


command = {
    "environment": "staging", "scenario": "bad-deployment", "percentage": 100,
    "duration_seconds": 180,
    "reason": "Verify synthetic bad release, payment degradation and rollback recovery",
}
creation_key = str(uuid.uuid4())
status, run = call("/simulations", command, creation_key)
assert status == 201 and run["active"] and run["deployment_id"], run
failed = None
try:
    _, replay = call("/simulations", command, creation_key)
    assert replay["id"] == run["id"] and replay["deployment_id"] == run["deployment_id"]
    detail = until(lambda: (lambda d: d if d["deployment_report"] == "Completed" else None)(call("/simulations/" + run["id"])[1]))
    assert detail["run"]["deployment_id"] == run["deployment_id"]
    _, deployment = call("/deployments/" + run["deployment_id"])
    assert deployment["status"] == "Completed" and deployment["service"] == "payment-service"
    assert deployment["version"] == "1.4.0-sim-bad" and len(deployment["events"]) == 1
    for _ in range(5):
        status, failed = purchase()
        assert status in (200, 201, 202) and failed["status"] == "FAILED", failed
        assert failed["error_code"] == "BAD_DEPLOYMENT_PAYMENT_FAILURE", failed
    _, unaffected = purchase("development")
    assert unaffected["status"] == "SUCCESS", unaffected
    _, counted = call("/simulations/" + run["id"])
    assert counted["selected"] >= 5 and counted["observed"] >= 5, counted
finally:
    _, stopped = call("/simulations/" + run["id"] + "/stop", {"reason": "Verification complete; roll back synthetic release"})
    assert not stopped["active"] and stopped["stopped_at"]

rolled_back = until(lambda: (lambda d: d if d["status"] == "Rolled Back" else None)(call("/deployments/" + run["deployment_id"])[1]))
assert [event["status"] for event in rolled_back["events"]] == ["Completed", "Rolled Back"]
_, recovered = purchase()
assert recovered["status"] == "SUCCESS", recovered
assert failed is not None
_, stable = call("/transactions/" + failed["id"])
assert stable["status"] == "FAILED" and stable["error_code"] == "BAD_DEPLOYMENT_PAYMENT_FAILURE"
print(json.dumps({
    "run_id": run["id"], "deployment_id": run["deployment_id"],
    "failed_transaction": failed["id"], "recovered_transaction": recovered["id"],
    "verified": ["durable release marker", "five failed purchases", "environment isolation", "rollback marker", "new purchase recovery", "stable failure history"],
}))
