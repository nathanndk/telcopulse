"""Exercise authenticated deployment intake, immutable history and gateway correlation."""

import datetime as dt
import json
import os
import subprocess
import urllib.parse
import urllib.request
import uuid


os.environ.setdefault("WEB_PORT", "3001")
base = "http://localhost:" + os.environ["WEB_PORT"]
suffix = uuid.uuid4().hex[:16]
deployment_id = "ci-payment-" + suffix
event_prefix = "ci-event-" + suffix
now = dt.datetime.now(dt.timezone.utc)
pending_at = now - dt.timedelta(minutes=4)
running_at = now - dt.timedelta(minutes=3)
completed_at = now - dt.timedelta(minutes=2)
rollback_at = now - dt.timedelta(minutes=1)


def timestamp(value):
    return value.isoformat().replace("+00:00", "Z")


def report(sequence, status, occurred_at, actor="ci:jenkins"):
    payload = {
        "event_id": event_prefix + "-" + str(sequence),
        "deployment_id": deployment_id,
        "service": "payment-service",
        "version": "1.4.0",
        "commit_sha": "abcdef0123456789abcdef0123456789abcdef01",
        "environment": "staging",
        "deployer": actor,
        "status": status,
        "occurred_at": timestamp(occurred_at),
    }
    command = [
        "docker", "compose", "exec", "-T", "-e", "PAYLOAD=" + json.dumps(payload),
        "deployment-service", "sh", "-c",
        'wget -q -S -O - --header="Authorization: Bearer $SERVICE_TOKEN" '
        '--header="Content-Type: application/json" --post-data="$PAYLOAD" '
        'http://127.0.0.1:8080/internal/deployments/events',
    ]
    result = subprocess.run(command, capture_output=True, text=True, timeout=20)
    return result


def query(path, params=None):
    url = base + path
    if params:
        url += "?" + urllib.parse.urlencode(params)
    with urllib.request.urlopen(url, timeout=10) as response:
        return json.load(response)


first = report(1, "Pending", pending_at)
assert first.returncode == 0 and "201 Created" in first.stderr, first.stderr
assert json.loads(first.stdout)["status"] == "Pending"
again = report(1, "Pending", pending_at)
assert again.returncode == 0 and "200 OK" in again.stderr, again.stderr
conflict = report(1, "Completed", pending_at)
assert conflict.returncode != 0 and "409 Conflict" in conflict.stderr, conflict.stderr
assert report(2, "Running", running_at).returncode == 0
assert report(3, "Completed", completed_at, "ci:gitlab").returncode == 0
same_time = report(6, "Rolled Back", completed_at, "operator:rollback")
assert same_time.returncode != 0 and "409 Conflict" in same_time.stderr, same_time.stderr

# Only the Pending event is in this window, but the current record is Completed.
window = {
    "environment": "staging", "service": "payment-service",
    "since": timestamp(pending_at - dt.timedelta(seconds=10)),
    "until": timestamp(pending_at + dt.timedelta(seconds=10)),
}
items = query("/api/v1/deployments", window)
assert any(item["id"] == deployment_id and item["status"] == "Completed" for item in items), items

assert report(4, "Rolled Back", rollback_at, "operator:rollback").returncode == 0
invalid = report(5, "Running", now)
assert invalid.returncode != 0 and "409 Conflict" in invalid.stderr, invalid.stderr
detail = query("/api/v1/deployments/" + deployment_id)
assert detail["status"] == "Rolled Back"
assert detail["deployer"] == "ci:jenkins"
assert [event["status"] for event in detail["events"]] == ["Pending", "Running", "Completed", "Rolled Back"]
assert [event["actor"] for event in detail["events"]][-1] == "operator:rollback"
items = query("/api/v1/deployments", window)
assert any(item["id"] == deployment_id for item in items), items
print("Deployment event replay, conflict, lifecycle, rollback audit and historical window correlation verified:", deployment_id)
