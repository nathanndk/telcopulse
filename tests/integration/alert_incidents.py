"""Detect a real Prometheus service outage and retain one operator-owned incident."""
import json
import time
import urllib.request
from kafka_recovery import BASE_URL, compose


def read(path):
    with urllib.request.urlopen(BASE_URL + path, timeout=5) as response:
        return json.load(response)


def records():
    items = []
    cursor = ""
    while True:
        page = read("/api/v1/incidents?environment=development&limit=100&cursor=" + cursor)
        items.extend(page["items"])
        if not page["more"]:
            return [i for i in items if i["title"] == "ServiceUnavailable · payment-service"]
        cursor = page["next"]


def firing():
    with urllib.request.urlopen("http://localhost:9090/api/v1/alerts", timeout=5) as response:
        return any(a["state"] == "firing" and a["labels"].get("alertname") == "ServiceUnavailable" and a["labels"].get("service") == "payment-service" for a in json.load(response)["data"]["alerts"])


assert not firing(), "Payment outage must not already be firing"
before = {i["id"] for i in records()}
compose("stop", "payment-service")
try:
    end = time.monotonic() + 100
    while time.monotonic() < end:
        new = [i for i in records() if i["id"] not in before]
        if new:
            assert firing(), "Incident appeared without a firing Prometheus rule"
            break
        time.sleep(1)
    else:
        raise AssertionError("No incident from the real firing alert")
    assert len(new) == 1
    incident_id = new[0]["id"]
    print("Real firing alert created " + incident_id, flush=True)
    time.sleep(22)  # More than two ingestion polls must not add a second incident.
    assert [i["id"] for i in records() if i["id"] not in before] == [incident_id]
    detail = read("/api/v1/incidents/" + incident_id)
    assert detail["incident"]["version"] == 1 and len(detail["history"]) == 1
    assert detail["history"][0]["actor"] == "prometheus-alert"
    assert detail["incident"]["affected_users"] is None
finally:
    compose("start", "payment-service")

end = time.monotonic() + 30
while firing() and time.monotonic() < end:
    time.sleep(1)
assert not firing(), "Payment alert did not recover"
remaining = read("/api/v1/incidents/" + incident_id)
assert remaining["incident"]["state"] == "Detected" and remaining["incident"]["version"] == 1
print(json.dumps({"incident_id": incident_id, "deduplicated": True, "alert_recovered": True, "operator_state_preserved": True}), flush=True)
