"""Stop Redis, then Kafka, and check dependency alerts and their inhibition.

Each dependency is stopped for about two minutes and restarted in a finally
block. While Redis is down new purchases are refused with 503; while Kafka is
down notifications wait in the outbox. Local Compose stack only.
"""
import json
import os
import subprocess
import time
import urllib.request

PROMETHEUS_URL = os.environ.get("PROMETHEUS_URL", "http://localhost:9090")
ALERTMANAGER_URL = os.environ.get("ALERTMANAGER_URL", "http://localhost:9093")


def get_json(url):
    with urllib.request.urlopen(url, timeout=10) as response:
        return json.load(response)


def prometheus_firing(alertname, **labels):
    alerts = get_json(PROMETHEUS_URL + "/api/v1/alerts")["data"]["alerts"]
    return [a for a in alerts if a["state"] == "firing" and a["labels"]["alertname"] == alertname and all(a["labels"].get(k) == v for k, v in labels.items())]


def alertmanager(alertname, **labels):
    alerts = get_json(ALERTMANAGER_URL + "/api/v2/alerts?active=true&inhibited=true&silenced=true")
    return [a for a in alerts if a["labels"]["alertname"] == alertname and all(a["labels"].get(k) == v for k, v in labels.items())]


def wait(predicate, seconds, message):
    end = time.monotonic() + seconds
    while time.monotonic() < end:
        value = predicate()
        if value:
            return value
        time.sleep(3)
    raise AssertionError(message)


def compose(*args):
    subprocess.run(["docker", "compose", *args], check=True, capture_output=True)


results = {}

# Redis: the down alert pages; the failing tcp probe of the same dependency is inhibited.
assert not prometheus_firing("RedisDown"), "RedisDown already firing"
compose("stop", "redis")
try:
    wait(lambda: prometheus_firing("RedisDown"), 120, "RedisDown did not fire")
    wait(lambda: prometheus_firing("DependencyProbeFailed", dependency="redis"), 120, "redis probe did not fail")
    probe = wait(lambda: [a for a in alertmanager("DependencyProbeFailed", dependency="redis") if a["status"]["inhibitedBy"]], 60, "redis probe alert was not inhibited")
    down = alertmanager("RedisDown")
    assert down and not down[0]["status"]["inhibitedBy"] and "oncall-page" in [r["name"] for r in down[0]["receivers"]], down
    results["redis"] = {"down_alert": "RedisDown", "probe_inhibited_by": probe[0]["status"]["inhibitedBy"]}
finally:
    compose("start", "redis")
wait(lambda: not prometheus_firing("RedisDown"), 120, "RedisDown did not resolve")

# Kafka: broker loss pages once; consumer-lag and publication symptoms are inhibited.
assert not prometheus_firing("KafkaBrokerUnavailable"), "KafkaBrokerUnavailable already firing"
compose("stop", "kafka")
try:
    wait(lambda: prometheus_firing("KafkaBrokerUnavailable"), 180, "KafkaBrokerUnavailable did not fire")
    kafka = wait(lambda: alertmanager("KafkaBrokerUnavailable"), 60, "Kafka alert did not reach Alertmanager")
    assert not kafka[0]["status"]["inhibitedBy"], kafka
    symptoms = [a for name in ("KafkaLagMeasurementUnavailable", "NotificationConsumerLag", "EventPublicationDelayed") for a in alertmanager(name)]
    assert all(a["status"]["inhibitedBy"] for a in symptoms), symptoms
    results["kafka"] = {"down_alert": "KafkaBrokerUnavailable", "inhibited_symptoms": sorted({a["labels"]["alertname"] for a in symptoms})}
finally:
    compose("start", "kafka")
wait(lambda: not prometheus_firing("KafkaBrokerUnavailable"), 240, "KafkaBrokerUnavailable did not resolve")
print(json.dumps(results), flush=True)
