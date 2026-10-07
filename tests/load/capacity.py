"""Find the purchase path's saturation point on the local Compose stack.

Runs tests/load/saturation.js at increasing constant arrival rates (default
5, 10, 20, 40, 60, 80 purchases/s for 45 s each, with a 15 s rest) and, for every
step, reads from Prometheus the busiest Go process CPU, gateway and payment
DB pool use, PostgreSQL connections, Kafka consumer lag and outbox age. It
stops after the first step that breaches the latency SLO (p95 > 2 s), loses
more than 1% of offered purchases, or drops more than 1% of iterations. The gateway purchase
budget is raised for the run and restored to the Compose default in finally.

Creates thousands of synthetic staging purchases. Local stack only.
"""
import json
import os
import subprocess
import time
import urllib.parse
import urllib.request

REPO = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
PROMETHEUS_URL = os.environ.get("PROMETHEUS_URL", "http://localhost:9090")
RATES = [int(r) for r in os.environ.get("RATES", "5,10,20,40,60,80").split(",")]
DURATION = int(os.environ.get("STEP_SECONDS", "45"))


def promql(expression):
    url = PROMETHEUS_URL + "/api/v1/query?" + urllib.parse.urlencode({"query": expression})
    with urllib.request.urlopen(url, timeout=10) as response:
        result = json.load(response)["data"]["result"]
    return result


def scalar(expression):
    rows = promql(expression)
    return round(float(rows[0]["value"][1]), 3) if rows else None


def compose(*args, env=None):
    subprocess.run(["docker", "compose", *args], cwd=REPO, check=True, capture_output=True, env=env)


def step(rate):
    with open(os.path.join(REPO, "tests/load/saturation.js")) as script:
        run = subprocess.run(["docker", "run", "--rm", "-i", "--network", "telcopulse_frontend", "-e", f"RATE={rate}", "-e", f"DURATION_SECONDS={DURATION}",
                              "grafana/k6:2.2.0", "run", "--quiet", "-"], stdin=script, capture_output=True, text=True)
    metrics = json.loads(run.stdout.strip().splitlines()[-1])
    value = lambda name, field="count": metrics.get(name, {}).get("values", {}).get(field, 0)
    window = f"{DURATION}s"
    cpu = promql(f'topk(3, rate(process_cpu_seconds_total{{job="telcopulse"}}[{window}]))')
    offered = rate * DURATION
    result = {
        "rate": rate,
        "offered": offered,
        "completed": int(value("purchase_completed")),
        "rejected": int(value("purchase_rejected")),
        "unresolved": int(value("purchase_unresolved")),
        "dropped": int(value("dropped_iterations")),
        "technical_failure_rate": round(value("purchase_technical_failure_rate", "rate"), 4),
        "p50_ms": round(value("purchase_end_to_end_ms", "med")),
        "p95_ms": round(value("purchase_end_to_end_ms", "p(95)")),
        "p99_ms": round(value("purchase_end_to_end_ms", "p(99)")),
        "achieved_per_s": round(value("purchase_completed") / DURATION, 1),
        "top_cpu_cores": {r["metric"]["service"]: round(float(r["value"][1]), 2) for r in cpu},
        "gateway_pool_max": scalar(f'max_over_time(database_connection_usage{{service="api-gateway"}}[{window}])'),
        "payment_pool_max": scalar(f'max_over_time(database_connection_usage{{service="payment-service"}}[{window}])'),
        "postgres_connections_max": scalar(f"max_over_time(sum(pg_stat_activity_count)[{window}:5s])"),
        "kafka_lag_max": scalar(f"max_over_time(sum(kafka_consumer_lag)[{window}:5s])"),
        "outbox_age_max_s": scalar(f"max(max_over_time(event_outbox_oldest_age_seconds[{window}]))"),
        "host_cpu_busy": scalar(f'1 - avg(rate(node_cpu_seconds_total{{mode="idle"}}[{window}]))'),
        "host_load1_after": float(open("/proc/loadavg").read().split()[0]),
        "host_swap_in_per_s": scalar(f"rate(node_vmstat_pswpin[{window}])"),
    }
    lost = offered - result["completed"]
    result["breach"] = [reason for reason, hit in (
        ("p95 above 2 s", result["p95_ms"] > 2000),
        ("more than 1% of offered purchases not completed", lost > 0.01 * offered),
        ("more than 1% of iterations dropped", result["dropped"] > 0.01 * offered),
        ("technical failures above 0.1%", result["technical_failure_rate"] > 0.001),
    ) if hit]
    return result


def host_calm():
    # A thrashing workstation measures the host, not TelcoPulse: wait for it to settle.
    deadline = time.monotonic() + 600
    while float(open("/proc/loadavg").read().split()[0]) > 8:
        if time.monotonic() > deadline:
            raise SystemExit("host load stayed above 8; capacity results would be meaningless")
        time.sleep(10)


env = dict(os.environ, PURCHASES_PER_MINUTE="100000")
compose("up", "-d", "--wait", "api-gateway", env=env)
host_calm()
# Warm the restarted gateway's pools and caches; not measured.
with open(os.path.join(REPO, "tests/load/saturation.js")) as warmup:
    subprocess.run(["docker", "run", "--rm", "-i", "--network", "telcopulse_frontend", "-e", "RATE=2", "-e", "DURATION_SECONDS=15",
                    "grafana/k6:2.2.0", "run", "--quiet", "-"], stdin=warmup, capture_output=True)
results = []
try:
    for rate in RATES:
        result = step(rate)
        results.append(result)
        print(json.dumps(result), flush=True)
        if result["breach"]:
            break
        time.sleep(15)
        host_calm()
finally:
    compose("up", "-d", "--wait", "api-gateway")
healthy = [r for r in results if not r["breach"]]
print(json.dumps({"max_healthy_rate": healthy[-1]["rate"] if healthy else None, "first_breach": next((r for r in results if r["breach"]), None) and results[-1]["rate"]}), flush=True)
