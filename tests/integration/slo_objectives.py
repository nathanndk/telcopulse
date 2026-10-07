"""Burn the purchase-latency and notification-freshness budgets and gate a release.

1. A bounded 100% database-latency simulation in staging makes eight purchases
   slower than 2 s. The test expects PurchaseLatencyBudgetBurn, an ingested
   incident, slow purchases in the durable 30-day latency SLO, and an error
   budget gate that blocks STAGING while DEV stays advisory.
2. A bounded kafka-consumer-lag simulation in development delays ten
   notifications one after another. The test expects late deliveries in
   notification_delivery_freshness_seconds and NotificationFreshnessBudgetBurn.

Both simulations stop in finally blocks. Local synthetic stack only; the
script never edits or resolves an incident.
"""
import os
import subprocess
import urllib.error
import uuid
import slo_burn
from slo_burn import WEB_URL, PROMETHEUS_URL, get_json, query, wait_for

REPO = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))


def call(path, data=None, key=None):
    try:
        return slo_burn.call(path, data, key)
    except urllib.error.HTTPError as error:
        raise AssertionError(f"{path} returned {error.code}: {error.read().decode()[:300]}") from None


def value(expression):
    rows = query(expression)
    return float(rows[0]["value"][1]) if rows else 0.0


def purchase(environment):
    payload = {"customer_id": "cus-001", "package_id": "pkg-10", "payment_method": "E-Wallet", "environment": environment}
    _, result = call("/transactions", payload, uuid.uuid4().hex)
    return result


def simulate(environment, scenario, delay_ms, reason):
    _, run = call("/simulations", {"environment": environment, "scenario": scenario, "percentage": 100, "duration_seconds": 180, "delay_ms": delay_ms, "reason": reason}, uuid.uuid4().hex)
    assert run["active"], run
    return run


def stop(run, reason):
    _, stopped = call("/simulations/" + run["id"] + "/stop", {"reason": reason})
    assert not stopped["active"], stopped


def gate(target):
    env = dict(os.environ, TARGET=target, SLO_PROMETHEUS_URL=PROMETHEUS_URL)
    env.pop("BUDGET_OVERRIDE_REASON", None)
    return subprocess.run(["bash", os.path.join(REPO, "scripts/error-budget-gate.sh")], env=env, capture_output=True, text=True)


def firing(alert, environment):
    return query(f'ALERTS{{alertname="{alert}",alertstate="firing",environment="{environment}"}}')


def incident(title, environment):
    page = get_json(WEB_URL + f"/api/v1/incidents?environment={environment}&state=Detected&limit=100")
    return next((item for item in page["items"] if item["title"] == title), None)


# 1. Latency objective in staging.
# A previous episode clears once its 5m window holds no slow purchases.
wait_for(lambda: not firing("PurchaseLatencyBudgetBurn", "staging"), "earlier latency episode to clear", timeout=480)
# Earlier runs may already have used the retained-window budget; report, do not assume.
before_gate = gate("STAGING")
print("gate before burn:", "pass" if before_gate.returncode == 0 else "blocked", (before_gate.stdout + before_gate.stderr).strip().replace("\n", " | "), flush=True)
slow_before = get_json(WEB_URL + "/api/v1/overview?environment=staging")["purchase_latency_slo"]["slow"]
run = simulate("staging", "database-latency", 2500, "Verify purchase latency SLO burn and release gate")
try:
    results = [purchase("staging") for _ in range(8)]
finally:
    stop(run, "Latency SLO verification complete")
assert all(r["status"] == "SUCCESS" and r["duration_ms"] > 2000 for r in results), [(r["status"], r["duration_ms"]) for r in results]
wait_for(lambda: firing("PurchaseLatencyBudgetBurn", "staging"), "PurchaseLatencyBudgetBurn firing", timeout=150)
latency = get_json(WEB_URL + "/api/v1/overview?environment=staging")["purchase_latency_slo"]
assert latency["slow"] >= slow_before + 8 and latency["threshold_ms"] == 2000, latency
blocked = gate("STAGING")
assert blocked.returncode != 0 and "burn alert firing: PurchaseLatencyBudgetBurn" in blocked.stderr, (blocked.stdout, blocked.stderr)
production_like = gate("PRODUCTION_LIKE")
assert production_like.returncode != 0, production_like.stderr
advisory = gate("DEV")
assert advisory.returncode == 0, advisory.stderr
found = wait_for(lambda: incident("PurchaseLatencyBudgetBurn · api-gateway", "staging"), "latency incident", timeout=60)
print(f"latency: {latency['slow']} slow of {latency['total']} durable staging purchases; gate blocked STAGING and PRODUCTION_LIKE; incident {found['id']}", flush=True)

# 2. Freshness objective in development.
wait_for(lambda: not firing("NotificationFreshnessBudgetBurn", "development"), "earlier freshness episode to clear", timeout=480)
count = 'sum(notification_delivery_freshness_seconds_count{environment="development"})'
fresh = r'sum(notification_delivery_freshness_seconds_bucket{environment="development",le=~"30(\\.0)?"})'
delivered_before, fresh_before = value(count), value(fresh)
run = simulate("development", "kafka-consumer-lag", 4500, "Verify notification freshness SLO burn")
try:
    for _ in range(10):
        purchase("development")
    # Keep the run active until the queued records have all started their waits.
    wait_for(lambda: value(count) >= delivered_before + 8, "delayed notification deliveries", timeout=120)
finally:
    stop(run, "Freshness SLO verification complete")
wait_for(lambda: value(count) >= delivered_before + 10, "all ten deliveries", timeout=120)
late = (value(count) - delivered_before) - (value(fresh) - fresh_before)
assert late >= 2, f"expected late deliveries, measured {late}"
wait_for(lambda: firing("NotificationFreshnessBudgetBurn", "development"), "NotificationFreshnessBudgetBurn firing", timeout=150)
found = wait_for(lambda: incident("NotificationFreshnessBudgetBurn · notification-service", "development"), "freshness incident", timeout=60)
print(f"freshness: {late:.0f} of 10 notifications later than 30 s; incident {found['id']}", flush=True)
