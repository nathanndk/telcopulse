"""Verify local Compose notification recovery; creates synthetic purchases."""

import json
import os
import subprocess
import time
import urllib.request
import urllib.error
import uuid

os.environ.setdefault("WEB_PORT", "3000")
BASE_URL = "http://localhost:" + os.environ["WEB_PORT"]


def compose(*args):
    return subprocess.check_output(["docker", "compose", *args], text=True).strip()


def sql(query):
    return compose(
        "exec", "-T", "postgres", "psql", "-U", "telcopulse", "-d", "telcopulse",
        "-Atc", query,
    )


def purchase():
    request = urllib.request.Request(
        BASE_URL + "/api/v1/transactions",
        data=json.dumps({
            "customer_id": "cus-001",
            "package_id": "pkg-10",
            "payment_method": "E-Wallet",
            "environment": "development",
        }).encode(),
        headers={
            "Content-Type": "application/json",
            "Idempotency-Key": uuid.uuid4().hex,
            "Origin": BASE_URL,
        },
    )
    with urllib.request.urlopen(request, timeout=15) as response:
        result = json.load(response)
    assert result["status"] == "SUCCESS", result
    # IDs enter SQL below; constrain them to the server's generated format.
    transaction_id = result["id"]
    assert transaction_id.startswith("TXN-") and len(transaction_id) == 28
    assert all(c in "0123456789abcdef" for c in transaction_id[4:])
    return transaction_id


def wait(query, expected, seconds=40):
    end = time.monotonic() + seconds
    while time.monotonic() < end:
        if sql(query) == expected:
            return
        time.sleep(0.5)
    raise AssertionError("timed out: " + query)


def delivery_count(transaction_id):
    return (
        "SELECT count(*) FROM notification.deliveries "
        f"WHERE transaction_id='{transaction_id}'"
    )


def notification_status(transaction_id):
    with urllib.request.urlopen(
        BASE_URL + f"/api/v1/transactions/{transaction_id}/notification", timeout=8
    ) as response:
        assert response.status == 200
        return json.load(response)


def event_counts(transaction_id):
    for table, expected in [("payment.event_outbox", "2"), ("notification.event_outbox", "1")]:
        wait(
            f"SELECT count(*) FROM {table} WHERE partition_key='{transaction_id}' "
            "AND published_at IS NOT NULL", expected,
        )


def main():
    compose("stop", "notification-service")
    try:
        transaction_id = purchase()
        wait(
            "SELECT count(*) FROM event_outbox "
            f"WHERE partition_key='{transaction_id}' AND published_at IS NOT NULL",
            "1",
        )
        assert sql(delivery_count(transaction_id)) == "0"
        try:
            notification_status(transaction_id)
            raise AssertionError("stopped service must not be reported as awaiting delivery")
        except urllib.error.HTTPError as error:
            assert error.code == 503
        print("Consumer offline: purchase succeeded and event published", flush=True)
    finally:
        compose("start", "notification-service")
    wait(delivery_count(transaction_id), "1")
    delivered = notification_status(transaction_id)
    assert delivered["transaction_id"] == transaction_id and delivered["status"] == "DELIVERED"
    assert delivered["delivered_at"]
    event_counts(transaction_id)
    print("Consumer resumed: one delivery and all domain events published", flush=True)

    compose("pause", "kafka")
    try:
        transaction_id = purchase()
        wait(
            "SELECT count(*) FROM event_outbox "
            f"WHERE partition_key='{transaction_id}' "
            "AND attempts>0 AND published_at IS NULL",
            "1", seconds=20,
        )
        assert notification_status(transaction_id) == {
            "transaction_id": transaction_id,
            "status": "AWAITING_DELIVERY",
            "delivered_at": None,
        }
        print("Broker offline: purchase succeeded and retry persisted", flush=True)
    finally:
        compose("unpause", "kafka")
    wait(delivery_count(transaction_id), "1", seconds=60)
    assert notification_status(transaction_id)["status"] == "DELIVERED"
    event_counts(transaction_id)
    print("Broker resumed: one delivery and all domain events published", flush=True)


if __name__ == "__main__":
    main()
