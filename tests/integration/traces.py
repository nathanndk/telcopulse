"""Verify a real purchase trace exported by the Compose services into Jaeger."""
import json
import os
import time
import urllib.error
import urllib.request
from kafka_recovery import purchase, sql

base = os.environ.get("JAEGER_URL", "http://localhost:16686")
transaction_id = purchase()
trace_id = sql(f"SELECT trace_id FROM transactions WHERE id='{transaction_id}'")
assert len(trace_id) == 32 and all(c in "0123456789abcdef" for c in trace_id)
end = time.monotonic() + 45
while time.monotonic() < end:
    try:
        with urllib.request.urlopen(base + "/api/traces/" + trace_id, timeout=5) as response:
            data = json.load(response)
        traces = data.get("data", [])
        if traces:
            trace = traces[0]
            spans = trace["spans"]
            processes = trace["processes"]
            services = {process["serviceName"] for process in processes.values()}
            if {"api-gateway", "subscriber-service", "package-service", "payment-service", "notification-service"} <= services:
                root = next((span for span in spans if span["operationName"] == "POST /api/v1/transactions"), None)
                names = {span["operationName"] for span in spans}
                required = {"publish telcopulse.notification.delivered.v1", "publish telcopulse.payment.state.v1", "publish telcopulse.purchase.completed.v1", "process telcopulse.purchase.completed.v1"}
                if root and required <= names and any(name.startswith("postgresql ") for name in names):
                    break
    except urllib.error.HTTPError as error:
        if error.code != 404:
            raise
    time.sleep(1)
else:
    raise AssertionError("Complete HTTP/database purchase trace not exported")

span_ids = {span["spanID"] for span in spans}
assert len(span_ids) == len(spans)
for span in spans:
    assert span["traceID"] == trace_id
    assert span["duration"] >= 0
    for ref in span.get("references", []):
        if ref["refType"] == "CHILD_OF":
            assert ref["spanID"] in span_ids, ("missing parent", span["operationName"])
    for tag in span.get("tags", []):
        assert tag["key"] not in {"db.statement", "db.query.text", "url.full", "http.url"}
print(f"Retrieved {len(spans)} connected spans across {len(services)} services")
print(base + "/trace/" + trace_id)
