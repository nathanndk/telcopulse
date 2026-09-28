"""Verify a poison purchase event reaches the redacted Kafka dead-letter topic."""

import hashlib
import json
import subprocess
import time
import uuid

SOURCE_TOPIC = "telcopulse.purchase.completed.v1"
DEAD_TOPIC = "telcopulse.notification.dead-letter.v1"


def compose(*args, input_bytes=None):
    return subprocess.run(
        ["docker", "compose", *args],
        input=input_bytes,
        capture_output=True,
        check=True,
    ).stdout.decode().strip()


def sql(query):
    return compose(
        "exec", "-T", "postgres", "psql", "-U", "telcopulse", "-d", "telcopulse", "-Atc", query
    )


def wait(query, expected, seconds=35):
    end = time.monotonic() + seconds
    while time.monotonic() < end:
        if sql(query) == expected:
            return
        time.sleep(0.5)
    raise AssertionError("timed out waiting for dead-letter publication")


def main():
    marker = uuid.uuid4().hex
    raw = json.dumps({"invalid_probe": marker}, separators=(",", ":")).encode()
    compose(
        "exec", "-T", "kafka", "/opt/kafka/bin/kafka-console-producer.sh",
        "--bootstrap-server", "localhost:9092", "--topic", SOURCE_TOPIC,
        input_bytes=raw + b"\n",
    )
    source = sql(
        "SELECT topic || '|' || partition_id || '|' || message_offset "
        "FROM notification.dead_letters WHERE payload=decode('" + raw.hex() + "','hex')"
    )
    if not source:
        wait(
            "SELECT count(*) FROM notification.dead_letters WHERE payload=decode('" + raw.hex() + "','hex')",
            "1",
        )
        source = sql(
            "SELECT topic || '|' || partition_id || '|' || message_offset "
            "FROM notification.dead_letters WHERE payload=decode('" + raw.hex() + "','hex')"
        )
    topic, partition, offset = source.split("|")
    assert topic == SOURCE_TOPIC
    event_id = f"notification.dead-letter:{topic}:{partition}:{offset}"
    wait(
        "SELECT published_at IS NOT NULL FROM notification.event_outbox WHERE event_id='" + event_id + "'",
        "t",
    )
    stored = json.loads(sql(
        "SELECT payload::text FROM notification.event_outbox WHERE event_id='" + event_id + "'"
    ))
    assert stored["type"] == DEAD_TOPIC
    assert stored["source_topic"] == SOURCE_TOPIC
    assert stored["source_partition"] == int(partition)
    assert stored["source_offset"] == int(offset)
    assert stored["reason"] == "invalid purchase event"
    assert stored["payload_bytes"] == len(raw)
    assert stored["payload_sha256"] == hashlib.sha256(raw).hexdigest()
    assert marker not in json.dumps(stored)

    consumed = subprocess.run(
        ["docker", "compose", "exec", "-T", "kafka", "/opt/kafka/bin/kafka-console-consumer.sh",
         "--bootstrap-server", "localhost:9092", "--topic", DEAD_TOPIC,
         "--from-beginning", "--timeout-ms", "10000"],
        capture_output=True, text=True, timeout=20,
    )
    records = [json.loads(line) for line in consumed.stdout.splitlines() if line.startswith("{")]
    matches = [record for record in records if record.get("event_id") == event_id]
    assert matches, f"dead-letter event missing from Kafka topic ({len(records)} records inspected)"
    assert all(marker not in json.dumps(record) for record in records)
    assert matches[0] == stored
    print(f"Dead-letter event {event_id} published without raw payload", flush=True)


if __name__ == "__main__":
    main()
