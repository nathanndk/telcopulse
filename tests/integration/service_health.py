"""Verify a real scrape failure and recovery through the public service-health API."""
import json
import os
import subprocess
import time
import urllib.request

BASE = os.environ.get("WEB_URL", "http://localhost:3001")


def payment():
    with urllib.request.urlopen(BASE + "/api/v1/services/health", timeout=10) as response:
        data = json.load(response)
    assert data["scope"] == "shared-runtime"
    return next(item for item in data["items"] if item["id"] == "payment-service")


def wait_for(up):
    deadline = time.monotonic() + 25
    while time.monotonic() < deadline:
        item = payment()
        if item["up"] == up:
            return item
        time.sleep(2)
    raise AssertionError(f"payment scrape did not reach {up}")


assert wait_for(1)["up"] == 1
try:
    subprocess.run(["docker", "compose", "stop", "payment-service"], check=True)
    failed = wait_for(0)
    assert failed["status"] == "Critical", failed
    assert failed["reason"] == "Prometheus scrape failed", failed
finally:
    subprocess.run(["docker", "compose", "up", "-d", "--no-build", "payment-service"], check=True)
assert wait_for(1)["status"] != "Critical", "scrape recovered but status remained critical"
print("Verified live service scrape failure and recovery")
