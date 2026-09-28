"""Verify real delayed purchases -> firing latency alert -> deduplicated incident."""
from datetime import datetime
import json
import os
import time
import urllib.parse
import urllib.request
import uuid

BASE = os.environ.get("WEB_URL", "http://localhost:3001")
PROM = os.environ.get("PROMETHEUS_URL", "http://localhost:9090")


def read(url):
    with urllib.request.urlopen(url, timeout=10) as response:
        return json.load(response)


def call(path, data=None):
    req = urllib.request.Request(BASE + "/api/v1" + path, data=None if data is None else json.dumps(data).encode(), headers={"Content-Type": "application/json", "Idempotency-Key": str(uuid.uuid4())})
    with urllib.request.urlopen(req, timeout=20) as response:
        return json.load(response)


def matching_incidents(active_at):
    cursor = ""
    matches = []
    while True:
        page = call("/incidents?environment=development&limit=20" + ("&cursor=" + urllib.parse.quote(cursor) if cursor else ""))
        for item in page["items"]:
            if item["title"] == "ServiceLatencyHigh · payment-service":
                detail = call("/incidents/" + item["id"])
                # Compare timestamps, accepting equivalent UTC representations.
                if datetime.fromisoformat(detail["incident"]["detected_at"].replace("Z", "+00:00")) == datetime.fromisoformat(active_at.replace("Z", "+00:00")):
                    matches.append(detail)
        if not page.get("next"):
            return matches
        cursor = page["next"]


run = call("/simulations", {"scenario": "database-latency", "environment": "staging", "percentage": 100, "duration_seconds": 240, "delay_ms": 4200, "reason": "Verify real sustained latency alert and incident ingestion"})
print(json.dumps({"started_run": run["id"]}), flush=True)
payload = {"customer_id": "cus-001", "package_id": "pkg-10", "payment_method": "E-Wallet", "environment": "staging"}
try:
    for index in range(24):
        txn = call("/transactions", payload)
        assert txn["status"] == "SUCCESS" and txn["duration_ms"] >= 4100, txn
        if (index + 1) % 6 == 0:
            print(json.dumps({"delayed_purchases": index + 1}), flush=True)
    deadline = time.monotonic() + 65
    alert = None
    while time.monotonic() < deadline:
        alerts = read(PROM + "/api/v1/alerts")["data"]["alerts"]
        alert = next((a for a in alerts if a["labels"].get("alertname") == "ServiceLatencyHigh" and a["labels"].get("service") == "payment-service" and a["state"] == "firing"), None)
        if alert:
            break
        time.sleep(2)
    assert alert, "Payment latency alert did not fire"
    deadline = time.monotonic() + 30
    matches = []
    while time.monotonic() < deadline:
        matches = matching_incidents(alert["activeAt"])
        if matches:
            break
        time.sleep(2)
    assert len(matches) == 1, matches
    incident = matches[0]["incident"]
    assert incident["severity"] == "SEV-2" and incident["state"] == "Detected", incident
    assert incident["latency_ms"] is None and "shared-runtime" in incident["evidence"][0]["summary"]
    time.sleep(22)  # Observe at least two additional ingestion polls.
    repeated = matching_incidents(alert["activeAt"])
    assert len(repeated) == 1 and repeated[0]["incident"]["id"] == incident["id"]
    assert len(repeated[0]["history"]) == 1, repeated
finally:
    stopped = call("/simulations/" + run["id"] + "/stop", {"reason": "Restore ordinary purchase latency after alert verification"})
    assert not stopped["active"]
recovered = call("/transactions", payload)
assert recovered["status"] == "SUCCESS" and recovered["duration_ms"] < 2000, recovered
print(json.dumps({"run_id": run["id"], "incident_id": incident["id"], "alert_active_at": alert["activeAt"], "recovered_ms": recovered["duration_ms"], "verified": "24 delayed purchases, firing alert, one incident across repeated polls, stopped run and recovered purchase"}))
