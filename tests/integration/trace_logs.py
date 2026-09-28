"""Verify purchase trace correlation across live Compose service logs."""
import json
import time
from kafka_recovery import compose, purchase, sql

transaction_id = purchase()
trace_id = sql(f"SELECT trace_id FROM transactions WHERE id='{transaction_id}'")
assert len(trace_id) == 32 and all(c in "0123456789abcdef" for c in trace_id)
expected = {"api-gateway", "subscriber-service", "package-service", "payment-service"}
end = time.monotonic() + 15
while time.monotonic() < end:
    found = set()
    for line in compose("logs", "--no-log-prefix", "--since", "1m", *sorted(expected)).splitlines():
        try:
            record = json.loads(line)
        except json.JSONDecodeError:
            continue
        if record.get("msg") == "HTTP request completed" and record.get("trace_id") == trace_id:
            assert len(record["span_id"]) == 16
            assert record["route"] != "unmatched"
            found.add(record["service"])
    if found == expected:
        print("Purchase trace ID correlates gateway, subscriber, package and payment HTTP logs")
        break
    time.sleep(0.5)
else:
    raise AssertionError(f"Missing correlated services: {expected - found}")
