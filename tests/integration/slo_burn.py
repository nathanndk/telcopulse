"""Exercise local synthetic purchase SLO burn through Prometheus and incident ingestion.

Starts a bounded 100% payment-decline simulation in development, creates six durable
purchases that fail with SIMULATED_PAYMENT_DECLINED (a technical failure that burns the
error budget), and always stops the simulation. Business declines such as
INSUFFICIENT_BALANCE deliberately do not burn the budget, so they cannot drive this test.
Run only against the disposable/local demo stack; the script never alters or resolves an
incident.
"""

import json
import os
import time
import urllib.parse
import urllib.request
import uuid
from datetime import datetime, timedelta, timezone


WEB_URL = os.environ.get("WEB_URL", "http://localhost:3001")
PROMETHEUS_URL = os.environ.get("PROMETHEUS_URL", "http://localhost:9090")


def get_json(url):
    with urllib.request.urlopen(url, timeout=10) as response:
        return json.load(response)


def query(expression):
    result = get_json(PROMETHEUS_URL + "/api/v1/query?" + urllib.parse.urlencode({"query": expression}))
    assert result["status"] == "success", result
    return result["data"]["result"]


def wait_for(check, description, timeout=120):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        value = check()
        if value:
            return value
        time.sleep(2)
    raise AssertionError("timed out waiting for " + description)


def call(path, data=None, key=None):
    headers = {"Content-Type": "application/json", "Origin": WEB_URL}
    if key:
        headers["Idempotency-Key"] = key
    request = urllib.request.Request(
        WEB_URL + "/api/v1" + path,
        data=None if data is None else json.dumps(data).encode(),
        headers=headers,
    )
    with urllib.request.urlopen(request, timeout=15) as response:
        return response.status, json.load(response)


def failed_purchase():
    payload = {"customer_id": "cus-001", "package_id": "pkg-10", "payment_method": "E-Wallet", "environment": "development"}
    status, purchase = call("/transactions", payload, uuid.uuid4().hex)
    deadline = time.monotonic() + 20
    while purchase["status"] == "PROCESSING" and time.monotonic() < deadline:
        time.sleep(1)
        _, purchase = call("/transactions/" + purchase["id"])
    assert status in (200, 201, 202) and purchase["status"] == "FAILED", purchase
    assert purchase["error_code"] == "SIMULATED_PAYMENT_DECLINED", purchase
    return purchase["id"]


def parse_time(value):
    return datetime.fromisoformat(value.replace("Z", "+00:00"))


def main():
    assert not query('ALERTS{alertname="BusinessSuccessRateLow",environment="development"}'), "alert already active"
    before = query('sum(transaction_total{environment="development",status="FAILED",failure_class="technical"})')
    baseline = float(before[0]["value"][1]) if before else 0.0
    wait_for(
        lambda: (rows := query('up{job="telcopulse",service="api-gateway"}')) and float(rows[0]["value"][1]) == 1,
        "healthy gateway scrape",
    )
    time.sleep(6)  # Establish a counter sample before the outcomes arrive.

    started_at = datetime.now(timezone.utc)
    _, run = call(
        "/simulations",
        {"environment": "development", "scenario": "payment-decline", "percentage": 100, "duration_seconds": 30, "reason": "Verify SLO burn from technical failures"},
        uuid.uuid4().hex,
    )
    assert run["active"], run
    try:
        purchase_ids = [failed_purchase() for _ in range(6)]
    finally:
        _, stopped = call("/simulations/" + run["id"] + "/stop", {"reason": "SLO burn verification complete; restore new purchases"})
        assert not stopped["active"] and stopped["stopped_at"], stopped
    wait_for(
        lambda: (rows := query('sum(transaction_total{environment="development",status="FAILED",failure_class="technical"})')) and float(rows[0]["value"][1]) >= baseline + 6,
        "six technical failures in Prometheus",
    )
    wait_for(
        lambda: (rows := query('telcopulse:business_failure_ratio:increase5m{environment="development"}')) and float(rows[0]["value"][1]) > 0.0144,
        "fast-window budget burn",
    )
    wait_for(
        lambda: next((row for row in query('ALERTS{alertname="BusinessSuccessRateLow",alertstate="firing",environment="development"}')), None),
        "firing SLO burn alert",
    )
    alerts = get_json(PROMETHEUS_URL + "/api/v1/alerts")["data"]["alerts"]
    alert = next(item for item in alerts if item["labels"].get("alertname") == "BusinessSuccessRateLow" and item["labels"].get("environment") == "development" and item["state"] == "firing")
    active_at = parse_time(alert["activeAt"])
    assert active_at >= started_at - timedelta(seconds=5), "alert predates this test"

    def ingested_incident():
        page = get_json(WEB_URL + "/api/v1/incidents?environment=development&state=Detected&limit=100")
        return next((item for item in page["items"] if item["title"] == "BusinessSuccessRateLow · api-gateway" and abs((parse_time(item["detected_at"]) - active_at).total_seconds()) < 1), None)

    incident = wait_for(ingested_incident, "incident from SLO burn")
    detail = get_json(WEB_URL + "/api/v1/incidents/" + incident["id"])["incident"]
    assert "paired 5m/1h or 30m/6h" in detail["evidence"][0]["summary"], detail
    print(f"Six simulated technical failures {purchase_ids[0]}…{purchase_ids[-1]} fired an environment-scoped SLO alert and incident {incident['id']}")


if __name__ == "__main__":
    main()
