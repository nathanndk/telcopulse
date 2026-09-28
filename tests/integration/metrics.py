"""Verify measured business failure, alerts and broker lag in local Compose."""
import json
import os
import time
import urllib.parse
import urllib.request
import uuid
from kafka_recovery import BASE_URL, compose, purchase

PROMETHEUS_URL = os.environ.get("PROMETHEUS_URL", "http://localhost:9090")

def query(expression):
    url = PROMETHEUS_URL + "/api/v1/query?" + urllib.parse.urlencode({"query": expression})
    with urllib.request.urlopen(url, timeout=10) as response:
        result = json.load(response)
    assert result["status"] == "success", result
    return result["data"]["result"]

def wait(expression, predicate, seconds=90):
    end = time.monotonic() + seconds
    while time.monotonic() < end:
        result = query(expression)
        if result and predicate(result):
            return result
        time.sleep(2)
    raise AssertionError("metric did not converge: " + expression)

def fail_purchase(key):
    request = urllib.request.Request(
        BASE_URL + "/api/v1/transactions",
        data=json.dumps({"customer_id": "cus-003", "package_id": "pkg-25", "payment_method": "Pulsa", "environment": "development"}).encode(),
        headers={"Content-Type": "application/json", "Idempotency-Key": key, "Origin": BASE_URL},
    )
    with urllib.request.urlopen(request, timeout=10) as response:
        result = json.load(response)
        assert result["status"] == "FAILED" and response.status in (200, 201), result
        return result["id"]

def main():
    wait('count(up{job="telcopulse"} == 1)', lambda rows: float(rows[0]["value"][1]) == 6)
    expression = 'sum(transaction_total{environment="development",status="FAILED"})'
    before = float(query(expression)[0]["value"][1])
    time.sleep(6)  # Ensure a scrape baseline exists before producing outcomes.
    key = uuid.uuid4().hex
    assert fail_purchase(key) == fail_purchase(key)
    for _ in range(5):
        fail_purchase(uuid.uuid4().hex)
    wait(expression, lambda rows: float(rows[0]["value"][1]) == before + 6)
    wait('ALERTS{alertname="BusinessSuccessRateLow",alertstate="firing",environment="development"}', lambda rows: bool(rows))
    print("Six business failures counted; replay excluded; SLO burn alert firing", flush=True)
    compose("stop", "notification-service")
    try:
        purchase()
        wait("sum(kafka_consumer_lag)", lambda rows: float(rows[0]["value"][1]) > 0)
        print("Stopped consumer produces measured broker lag", flush=True)
    finally:
        compose("start", "notification-service")
    wait("sum(kafka_consumer_lag)", lambda rows: float(rows[0]["value"][1]) == 0)
    print("Consumer recovery returns measured lag to zero", flush=True)

if __name__ == "__main__":
    main()
