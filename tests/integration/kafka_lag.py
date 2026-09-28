"""Exercise actual Kafka backlog growth and drain with bounded simulation cleanup."""
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
    with urllib.request.urlopen(url, timeout=15) as response:
        return json.load(response)


def call(path, data=None):
    request = urllib.request.Request(BASE + "/api/v1" + path, data=None if data is None else json.dumps(data).encode(), headers={"Content-Type": "application/json", "Idempotency-Key": str(uuid.uuid4())})
    with urllib.request.urlopen(request, timeout=20) as response:
        return json.load(response)


def lag():
    result = read(PROM + "/api/v1/query?" + urllib.parse.urlencode({"query": "sum(kafka_consumer_lag)"}))["data"]["result"]
    assert result, "Lag measurement missing"
    return float(result[0]["value"][1])


def wait_for(check, seconds):
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        result = check()
        if result:
            return result
        time.sleep(2)
    raise AssertionError("Timed out waiting for " + check.__name__)


wait_for(lambda: lag() == 0, 30)
run = call("/simulations", {"scenario": "kafka-consumer-lag", "environment": "staging", "percentage": 100, "duration_seconds": 180, "delay_ms": 4200, "reason": "Verify actual Kafka lag, alert and drain"})
print(json.dumps({"run_id": run["id"]}), flush=True)
payload = {"customer_id": "cus-001", "package_id": "pkg-10", "payment_method": "E-Wallet", "environment": "staging"}
try:
    for _ in range(40):
        transaction = call("/transactions", payload)
        assert transaction["status"] == "SUCCESS", transaction
    peak = wait_for(lambda: lag() if lag() > 10 else None, 30)
    print(json.dumps({"observed_lag": peak}), flush=True)

    def firing():
        alerts = read(PROM + "/api/v1/alerts")["data"]["alerts"]
        return next((a for a in alerts if a["labels"].get("alertname") == "NotificationConsumerLag" and a["state"] == "firing"), None)

    alert = wait_for(firing, 60)
    print(json.dumps({"alert_active_at": alert["activeAt"]}), flush=True)

    def detected():
        cursor = ""
        while True:
            page = call("/incidents?environment=development&limit=20" + ("&cursor=" + urllib.parse.quote(cursor) if cursor else ""))
            for item in page["items"]:
                if item["title"] == "NotificationConsumerLag · notification-service":
                    detail = call("/incidents/" + item["id"])["incident"]
                    if datetime.fromisoformat(detail["detected_at"].replace("Z", "+00:00")) == datetime.fromisoformat(alert["activeAt"].replace("Z", "+00:00")):
                        return detail
            cursor = page.get("next")
            if not cursor:
                return None

    incident = wait_for(detected, 25)
    assert incident["severity"] == "SEV-2"

finally:
    stopped = call("/simulations/" + run["id"] + "/stop", {"reason": "Restore normal notification throughput"})
    assert not stopped["active"]
wait_for(lambda: lag() == 0, 45)
print(json.dumps({"run_id": run["id"], "incident_id": incident["id"], "observed_lag": peak, "recovered_lag": lag(), "verified": "40 successful purchases, real backlog, firing lag alert, stop and backlog drain"}))
