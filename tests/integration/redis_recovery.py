"""Verify Redis failure policy against the local Compose stack."""

import json
import time
import urllib.error
import urllib.request
import uuid

from kafka_recovery import BASE_URL, compose, sql


def catalog():
    with urllib.request.urlopen(BASE_URL + "/api/v1/packages", timeout=5) as response:
        return json.load(response)


def request_purchase(key):
    request = urllib.request.Request(
        BASE_URL + "/api/v1/transactions",
        data=json.dumps({
            "customer_id": "cus-001", "package_id": "pkg-10",
            "payment_method": "E-Wallet", "environment": "development",
        }).encode(),
        headers={
            "Content-Type": "application/json", "Idempotency-Key": key,
            "Origin": BASE_URL,
        },
    )
    try:
        with urllib.request.urlopen(request, timeout=5) as response:
            return response.status, json.load(response)
    except urllib.error.HTTPError as error:
        return error.code, json.load(error)


def main():
    expected = catalog()
    assert expected and catalog() == expected
    ttl = int(compose("exec", "-T", "redis", "redis-cli", "TTL", "telcopulse:catalog:v1"))
    assert 0 < ttl <= 30, ttl
    before = sql("SELECT count(*) FROM purchase_workflows")
    key = uuid.uuid4().hex
    compose("pause", "redis")
    try:
        started = time.monotonic()
        assert catalog() == expected
        status, result = request_purchase(key)
        assert status == 503, (status, result)
        assert time.monotonic() - started < 3
        assert sql("SELECT count(*) FROM purchase_workflows") == before
        print("Redis paused: catalog fallback, bounded 503, no workflow created", flush=True)
    finally:
        compose("unpause", "redis")
    status, result = request_purchase(key)
    assert status == 201 and result["status"] == "SUCCESS", (status, result)
    print("Redis resumed: same-key purchase succeeds", flush=True)


if __name__ == "__main__":
    main()
