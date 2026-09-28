# Incident response runbooks

These guides apply to synthetic development/staging traffic and, where stated, an isolated Kubernetes evaluation namespace. They do not authorize changes to a customer-facing environment. Start with the incident's environment, detection time, service and alert source; preserve transaction and trace IDs as references, and record each finding and action in the incident audit trail. A selected simulation or a nearby deployment is a hypothesis until measurements corroborate it.

Use the [ITOC Overview](/) and [Incidents](/incidents) in the local console, the [Grafana operations dashboard](http://localhost:3002/d/telcopulse-operations), Prometheus on host loopback port 9090, and the [local trace viewer](http://localhost:16686) when running Compose. Never paste raw MSISDNs, bearer tokens, database URLs or payment details into incidents. A missing series, trace or vendor export means evidence is unavailable, not healthy.

1. [High error rate](high-error-rate.md)
2. [High latency](high-latency.md)
3. [Database connection exhaustion](database-connection-exhaustion.md)
4. [Kafka consumer lag](kafka-consumer-lag.md)
5. [Pod CrashLoopBackOff](pod-crashloopbackoff.md)
6. [OOMKilled](oomkilled.md)
7. [Dependency timeout](dependency-timeout.md)
8. [Bad deployment](bad-deployment.md)

For each response: acknowledge and assign the incident, establish impact with measured outcomes, capture time-aligned metrics/logs/traces and related deployments, document the suspected root cause and mitigation, then verify fresh transactions and relevant queues before resolution. After a SEV-1/2 resolution, generate the structured postmortem and assign corrective actions. The [incident contract](../incidents.md), [simulation guide](../simulations.md) and [observability guide](../observability.md) define the actual local behavior and limitations.

Splunk, Dynatrace and Datadog are optional or conceptual integrations here. Use their links only when the relevant environment is configured and check the destination's data scope. Local Compose cannot prove Kubernetes restarts or vendor indexing; the Kubernetes guides require a real isolated cluster and explicit workload evidence.
