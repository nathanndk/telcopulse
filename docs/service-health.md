# Service health

`GET /api/v1/services/health` reads fixed Prometheus instant queries through the Go gateway. Configure `PROMETHEUS_URL` on the gateway; Compose points it at local Prometheus. No browser-supplied URL or PromQL is forwarded. The adapter uses a single evaluation timestamp, four sequential queries, two-second request limits, an eight-second overall deadline, a 1 MiB response limit and at most 100 series per query. Redirects and partial/warning responses fail closed. The public API masks upstream errors as 503.

Measurements have `shared-runtime` scope: HTTP instrumentation has no transaction environment label. Development and staging use the same service processes. The UI explicitly reports that scope rather than implying environment isolation. Business success remains a separate purchase metric. HTTP 5xx excludes client errors and does not detect an HTTP-200 business failure.

Each service exposes nullable scrape availability, requests/second, HTTP 5xx fraction and histogram P95 milliseconds. HTTP metrics use a five-minute rate window; probes and scrapes are excluded by instrumentation. Scrape samples older than 30 seconds are excluded. Missing, negative and non-finite values remain null. No traffic is not proof of health. Login remains Unknown until the auth service exists and is scraped.

Status policy:

- Critical: a recent scrape failed, or HTTP server errors are at least 5%.
- Degraded: HTTP server errors are at least 1%, or HTTP P95 is at least one second.
- Healthy: recent successful scrape, positive observed traffic, and complete HTTP measurements within those thresholds.
- Unknown: absent or incomplete measurements, or no observed traffic.

These are console thresholds, not SLOs or alert rules. P95 is an estimate from configured histogram buckets. A successful scrape alone does not verify every dependency or asynchronous delivery. Notification consumer lag continues to require its own evidence; business outcomes and incidents remain separate signals. With multiple replicas, the minimum scrape result marks any unavailable target Critical; richer replica-aware policy remains future work.

Prometheus response shape and instant-query parameters follow the [official HTTP API](https://prometheus.io/docs/prometheus/latest/querying/api/#instant-queries).
