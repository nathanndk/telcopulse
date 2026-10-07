# On-call and alert routing policy

This policy covers the TelcoPulse evaluation environments (development, staging and the isolated production-like namespace). It describes how a firing Prometheus rule reaches a person, how fast that person must respond, and how alerts are silenced. It does not authorize changes to any customer-facing system.

## Alert path

```
Prometheus rules ──► Alertmanager ──┬─► incident-service webhook  (every alert except Watchdog)
                                    ├─► Telegram page / alert / silent ticket  (by severity)
                                    └─► external heartbeat  (Watchdog only, optional)
```

[Alertmanager](../infrastructure/alertmanager/alertmanager.yml) groups by `alertname`, `environment` and `service`. Every alert is sent first to incident-service on a dedicated listener (`:9094/alertmanager`) authenticated by `ALERT_WEBHOOK_TOKEN`. This token can only open detections; it cannot read or edit incidents, and it is separate from `SERVICE_TOKEN`. Incident-service opens incidents for the [nine application rules](incidents.md#automatic-alert-detection). Other rules, such as infrastructure dependency signals, notify people without creating an incident, because the affected application service still needs triage. Human routing then continues by severity:

| Severity | Meaning | Receiver | Group wait / repeat | Acknowledge within | Engage |
| --- | --- | --- | --- | --- | --- |
| SEV-1 | A whole service or the alerting path is down | `oncall-page`: Telegram with sound, prefixed `🔴 PAGE` | 10 s / 30 min | **5 minutes**, at any hour | Primary on-call. Escalate to secondary at 15 minutes without acknowledgement, and to the incident commander at 30 minutes. |
| SEV-2 | An SLO is burning fast, or a degradation will become customer-visible | `oncall-notify`: Telegram, prefixed `🟠 ALERT` | 30 s / 2 h | **15 minutes** in working hours, 30 minutes otherwise | Primary on-call. Escalate to secondary if not acknowledged within 30 minutes. |
| SEV-3 | Evidence loss or a measurement gap, with no direct impact | `ops-ticket`: silent Telegram message, prefixed `🟡 TICKET` | 2 min / 24 h | Next working day | Owning team triages it in the incident register. |
| none | `Watchdog` heartbeat | `heartbeat`: external dead-man's switch | 0 s / 1 min | n/a | Configure the external check to page the primary when posts stop for 5 minutes. |

"Acknowledge" means moving the TelcoPulse incident from Detected to Acknowledged and assigning an owner. Telegram has no acknowledgement action, so the incident lifecycle is the system of record. The ITOC Overview reports MTTA from those audit events (see [operational metrics](operational-metrics.md)). Review MTTA against the targets in the weekly handoff.

## Rotation and handoff

- **Primary and secondary**, weekly rotation that hands off at Monday 10:00 local time. Nobody holds primary in two consecutive weeks. The secondary is the previous week's primary, who knows the most recent context.
- **Incident commander** for SEV-1, and for any SEV-2 lasting more than an hour: the engineering lead on duty. The commander coordinates and communicates; the primary keeps investigating.
- **Handoff checklist**: open incidents and their owners; active silences, with expiry and reason (`amtool silence query`); error budget status of each SLO (see [error budget policy](error-budget-policy.md)); deployments in flight; runbook gaps found during the week.
- **Load guardrail**: if a shift has more than two out-of-hours pages, or more than 25% of pages were not actionable, the next planning cycle must take the alert-quality work before feature work.

## Inhibition

Alertmanager suppresses symptoms when their cause is already firing:

- `ServiceUnavailable` for a service inhibits `ServiceLatencyHigh`, `EventPublicationDelayed`, `LogExportLoss`, `NotificationConsumerLag` and `KafkaLagMeasurementUnavailable` **for the same `service` label**.
- A SEV-1 inhibits SEV-2 and SEV-3 alerts that have the same `alertname`, `environment` and `service`.
- Dependency rules add their own inhibitions; see [infrastructure observability](observability.md#infrastructure-signals).

An inhibited alert is neither paged nor ingested, so an outage produces one incident for the cause rather than one per symptom. Inhibited alerts remain visible in Prometheus and in the Alertmanager UI (filter: *Inhibited*).

## Silences

Use a silence only for planned work, or for a known, owned alert that is already being worked. Every silence needs a matcher that is as narrow as possible, a creator, a comment linking the change or incident, and an expiry of at most 4 hours (24 hours for a scheduled maintenance window). Never silence `Watchdog`, `AlertmanagerUnreachable` or `AlertmanagerNotificationsFailing`.

```sh
docker compose exec alertmanager amtool --alertmanager.url=http://127.0.0.1:9093 silence add \
  alertname=ServiceUnavailable service=payment-service \
  --duration=2h --author="$USER" --comment="CHG-123 payment-service schema migration"
docker compose exec alertmanager amtool --alertmanager.url=http://127.0.0.1:9093 silence query
docker compose exec alertmanager amtool --alertmanager.url=http://127.0.0.1:9093 silence expire SILENCE_ID
```

A silenced alert is also **not ingested as an incident**: a silence declares that the situation is expected and already owned. Expire the silence as soon as the work ends; the next notification cycle then delivers the alert if it is still firing.

## Meta-alerting

- `Watchdog` always fires. Point the optional heartbeat receiver at an external dead-man's switch so that a dead Prometheus, Alertmanager or network path pages someone.
- `AlertmanagerNotificationsFailing` (SEV-2) fires when any integration (Telegram, webhook) failed during the last ten minutes. A broken Telegram credential is visible here even though the page itself was lost.
- `AlertmanagerUnreachable` (SEV-1) fires when Prometheus has no Alertmanager or cannot deliver to it. No notification can leave the system in this state, so check Prometheus and Grafana directly during an incident.

## Enabling Telegram

1. Create a bot with [@BotFather](https://t.me/BotFather) and add it to the on-call group. Send one message in the group, then read the numeric chat ID (for example `-1001234567890`) from `https://api.telegram.org/bot<token>/getUpdates`.
2. Add `TELEGRAM_BOT_TOKEN=...` and `TELEGRAM_CHAT_ID=...` to the ignored `.env` file. Compose mounts them into Alertmanager as secret files under `/run/secrets/`. They never appear as Alertmanager environment variables or in the rendered configuration. Environment-sourced secrets are used because rootless Docker on this workstation refuses read-only bind mounts of file-sourced secrets.
3. Start Alertmanager with the overlay. The log line `Telegram notification enabled` confirms that both values were found.

```sh
docker compose -f compose.yaml -f infrastructure/alertmanager/telegram.compose.yaml up -d alertmanager
```

Messages are plain text with no parse mode, so label values need no escaping. Each one names the severity, rule, environment, service, start time and `runbook_url`. The heartbeat overlay `infrastructure/alertmanager/heartbeat.compose.yaml` reads `HEARTBEAT_URL` in the same way.

## Verification

- `bash tests/ci/alertmanager-config.sh` renders the configuration with and without the Telegram and heartbeat secrets, runs `amtool check-config`, checks the receiver chosen for each severity, and confirms inhibition on a running Alertmanager. It needs only Docker.
- With the Compose stack running, `python3 tests/integration/alert_routing.py` replaces the Telegram API with a local mock and stops payment-service and package-service. It checks that one incident is opened through the webhook, that the SEV-1 page has the expected bot path, chat ID, text and runbook, that a maintenance silence suppresses both the page and the incident for package-service, and that a resolved notification arrives after recovery while the incident stays Detected. Both services, the default configuration and the mock are restored in `finally` blocks.
