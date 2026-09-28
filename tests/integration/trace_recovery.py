"""Verify recovery as an independent Jaeger trace linked to the original request."""
import datetime
import json
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid
from kafka_recovery import BASE_URL, compose, sql

key = uuid.uuid4().hex
compose("stop", "payment-service")
try:
    request = urllib.request.Request(
        BASE_URL + "/api/v1/transactions",
        data=json.dumps({"customer_id": "cus-001", "package_id": "pkg-10", "payment_method": "E-Wallet", "environment": "development"}).encode(),
        headers={"Content-Type": "application/json", "Idempotency-Key": key, "Origin": BASE_URL},
    )
    with urllib.request.urlopen(request, timeout=15) as response:
        result = json.load(response)
        assert response.status == 202 and result["status"] == "PROCESSING"
finally:
    compose("start", "payment-service")

transaction_id = result["id"]
assert transaction_id.startswith("TXN-") and len(transaction_id) == 28
assert all(c in "0123456789abcdef" for c in transaction_id[4:])
end = time.monotonic() + 45
while time.monotonic() < end:
    with urllib.request.urlopen(BASE_URL + "/api/v1/transactions/" + transaction_id, timeout=5) as response:
        recovered = json.load(response)
    if recovered["status"] == "SUCCESS":
        break
    time.sleep(1)
else:
    raise AssertionError("Background recovery did not complete")
origin = sql(f"SELECT traceparent FROM purchase_workflows WHERE transaction_id='{transaction_id}'").split("-")
assert len(origin) == 4
now = datetime.datetime.now(datetime.timezone.utc)
query = urllib.parse.urlencode({"query.serviceName": "api-gateway", "query.operationName": "purchase.resume", "query.startTimeMin": (now - datetime.timedelta(minutes=2)).isoformat(), "query.startTimeMax": (now + datetime.timedelta(minutes=1)).isoformat(), "query.searchDepth": 100})
end = time.monotonic() + 30
while time.monotonic() < end:
    try:
        with urllib.request.urlopen("http://localhost:16686/api/v3/trace-summaries?" + query, timeout=5) as response:
            summaries = json.load(response).get("summaries", [])
        traces = []
        for summary in summaries:
            candidate = summary["traceId"]
            assert len(candidate) == 32 and all(c in "0123456789abcdef" for c in candidate)
            with urllib.request.urlopen("http://localhost:16686/api/traces/" + candidate, timeout=5) as response:
                traces.extend(json.load(response).get("data", []))
    except urllib.error.HTTPError as error:
        if error.code != 404:
            raise
        traces = []
    matches = [trace for trace in traces if trace["traceID"] != origin[1] and any(
        ref["refType"] == "FOLLOWS_FROM" and ref["traceID"] == origin[1] and ref["spanID"] == origin[2]
        for span in trace["spans"] if span["operationName"] == "purchase.resume"
        for ref in span.get("references", [])
    )]
    if matches:
        print("Background recovery completed in an independent trace linked to the original request")
        print("http://localhost:16686/trace/" + matches[0]["traceID"])
        break
    time.sleep(1)
else:
    raise AssertionError("Recovery trace link not found in Jaeger")
