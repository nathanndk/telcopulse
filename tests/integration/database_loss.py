"""Game day: lose the local database, page, restore from backup, measure RPO/RTO.

1. Purchase A, then take a backup with the scheduled job's image.
2. Purchase B after the backup: it is inside the RPO window and will be lost.
3. Drop the telcopulse database and wait for PostgresDown to fire.
4. Restore with scripts/restore-database.sh and check that A exists, B does not,
   a new purchase succeeds and PostgresDown resolves.

DESTRUCTIVE for the local Compose database: data written after step 1 is lost.
The restore script takes no safety backup here because the database is gone.
"""
import json
import os
import re
import subprocess
import time
import urllib.error
import urllib.request
import uuid

WEB_URL = os.environ.get("WEB_URL", "http://localhost:3000")
PROMETHEUS_URL = os.environ.get("PROMETHEUS_URL", "http://localhost:9090")
REPO = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))


def compose(*args):
    return subprocess.run(["docker", "compose", *args], cwd=REPO, check=True, capture_output=True, text=True).stdout


def purchase():
    request = urllib.request.Request(WEB_URL + "/api/v1/transactions", data=json.dumps({
        "customer_id": "cus-001", "package_id": "pkg-10", "payment_method": "E-Wallet", "environment": "development",
    }).encode(), headers={"Content-Type": "application/json", "Idempotency-Key": uuid.uuid4().hex, "Origin": WEB_URL})
    with urllib.request.urlopen(request, timeout=15) as response:
        result = json.load(response)
    assert result["status"] == "SUCCESS", result
    return result["id"]


def exists(transaction):
    try:
        with urllib.request.urlopen(WEB_URL + "/api/v1/transactions/" + transaction, timeout=10):
            return True
    except urllib.error.HTTPError as error:
        if error.code == 404:
            return False
        raise


def firing(alert):
    with urllib.request.urlopen(PROMETHEUS_URL + "/api/v1/alerts", timeout=10) as response:
        return any(a["state"] == "firing" and a["labels"]["alertname"] == alert for a in json.load(response)["data"]["alerts"])


def wait(predicate, seconds, message):
    end = time.monotonic() + seconds
    while time.monotonic() < end:
        if predicate():
            return
        time.sleep(3)
    raise AssertionError(message)


kept = purchase()
backup_log = compose("run", "--rm", "--no-deps", "-e", "BACKUP_ONCE=1", "postgres-backup")
backup = re.search(r"(telcopulse-\d{8}T\d{6}Z\.dump)", backup_log).group(1)
backup_at = time.monotonic()
lost = purchase()
print(f"backup {backup}; kept {kept}; written after backup {lost}", flush=True)

loss_at = time.monotonic()
compose("exec", "-T", "postgres", "psql", "-q", "-U", "telcopulse", "-d", "postgres", "-c", "DROP DATABASE telcopulse WITH (FORCE)")
wait(lambda: firing("PostgresDown"), 120, "PostgresDown did not fire after the database was dropped")
detected_s = time.monotonic() - loss_at
print(f"PostgresDown fired {detected_s:.0f} s after the loss", flush=True)

restore = subprocess.run(["bash", "scripts/restore-database.sh", backup, "--yes-replace-local-database"], cwd=REPO, check=True, capture_output=True, text=True, env=dict(os.environ, WEB_PORT=WEB_URL.rsplit(":", 1)[1]))
summary = json.loads(restore.stdout.strip().splitlines()[-1])
recovered_s = time.monotonic() - loss_at

assert exists(kept), "purchase taken before the backup is missing"
assert not exists(lost), "purchase written after the backup survived; restore did not replace the database"
new = purchase()
wait(lambda: not firing("PostgresDown"), 120, "PostgresDown did not resolve")
print(json.dumps({
    "backup": backup,
    "backup_to_loss_s": round(loss_at - backup_at, 1),
    "detection_s": round(detected_s, 1),
    "restore_ms": summary["restore_ms"],
    "write_outage_during_restore_ms": summary["write_outage_ms"],
    "loss_to_recovery_s": round(recovered_s, 1),
    "kept_present": True, "post_backup_purchase_lost": True, "new_purchase": new,
}), flush=True)
