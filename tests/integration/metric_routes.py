"""Verify normalized routes after real browser/purchase traffic in Compose."""
from metrics import wait

for service, route in [
    ("api-gateway", "POST /api/v1/transactions"),
    ("subscriber-service", "GET /internal/customers/{id}"),
]:
    expression = 'http_requests_total{service="' + service + '",route="' + route + '"}'
    wait(expression, lambda rows: any(float(row["value"][1]) > 0 for row in rows), seconds=30)
    print(service + ": normalized route measured")
