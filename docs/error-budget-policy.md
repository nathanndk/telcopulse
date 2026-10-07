# Error budget policy

This policy decides what the team does when an SLO's error budget runs low. It applies to the three TelcoPulse SLOs, defined as code in [`infrastructure/slo/telcopulse.yml`](../infrastructure/slo/telcopulse.yml) and described in [the SLO guide](slo.md). Like the rest of the platform, it governs synthetic development, staging and production-like evaluation traffic, not a customer contract.

| SLO | Objective (rolling 30 days) | Budget |
| --- | --- | --- |
| `purchase-success` | 99.9% of completed purchases end without a technical failure | 1 technical failure per 1,000 purchases |
| `purchase-latency` | 99% of completed purchases finish within 2 s, including recovery | 1 slow purchase per 100 |
| `notification-freshness` | 99% of notifications are delivered within 30 s of the purchase outcome | 1 late notification per 100 |

## Budget states and required actions

| State | Trigger | Required action |
| --- | --- | --- |
| **Healthy** | More than 25% of every budget remains, and no burn alert is firing | Normal feature delivery. Use the spare budget for planned risk: migrations, dependency upgrades, chaos experiments. |
| **At risk** | 25% or less of any budget remains, or a burn alert fired during the last 7 days | The on-call engineer reviews it in the weekly handoff. The next sprint reserves at least one reliability item for the affected SLO. Chaos experiments against that SLO pause. |
| **Exhausted** | 0% or less of any budget remains, or a burn alert is firing now | **Release freeze** for staging and production-like (enforced by the Jenkins gate below). Only reliability fixes, security fixes and rollbacks ship. The owning team prioritizes the postmortem's corrective actions over feature work until the budget recovers above 0% and no burn alert has fired for 24 hours. |
| **Repeatedly exhausted** | Exhausted in two consecutive 30-day windows | Escalate to the engineering lead. Either re-plan the quarter around reliability work, or formally renegotiate the objective with evidence. Lowering an objective only to unfreeze releases is not allowed. |

A single incident that consumes more than 20% of a budget requires a postmortem, regardless of severity. The postmortem must name a corrective action that prevents the same budget loss from recurring.

## Release gate

[`scripts/error-budget-gate.sh`](../scripts/error-budget-gate.sh) runs in the Jenkins `DEPLOY` action twice: before preflight and approval, and again immediately before Helm upgrades the release, because the budget can burn while a change waits for approval. It queries the Prometheus instance that monitors the target (`SLO_PROMETHEUS_URL_DEV`, `_STAGING` or `_PRODUCTION_LIKE` in the Jenkins job environment):

- `ALERTS{alertstate="firing"}` for `BusinessSuccessRateLow`, `PurchaseLatencyBudgetBurn` and `NotificationFreshnessBudgetBurn` in the target's environment;
- `slo:period_error_budget_remaining:ratio`, the Sloth period budget, for each SLO in that environment.

| Target | Environment checked | Effect of a finding |
| --- | --- | --- |
| `DEV` | development | Advisory: reported in the log, deploy continues. Development is where fixes are proven. |
| `STAGING` | staging | Blocks the deploy. |
| `PRODUCTION_LIKE` | staging (its data scope) | Blocks the deploy. |

The gate **fails closed**. If Prometheus does not answer, or no `slo:objective:ratio` series exist because the SLO rules are not deployed, the promotion is blocked. An SLO with no completed traffic in the retained window has no budget series, because the generated SLIs skip idle windows rather than record `0/0`. The gate reports that budget as unknown and does not block on it. `ROLLBACK` and `SMOKE` are never gated, because they are how the budget is protected.

To ship a reliability fix during a freeze, set the Jenkins parameter `BUDGET_OVERRIDE_REASON` to one line of at least 20 characters naming the incident or fix (for example `INC-42 fix payment timeout regression`). The gate prints an `ERROR BUDGET OVERRIDE` line with the Jenkins user ID to the build log, and the production-like approval step still applies. Record every override in the related incident. More than one override per freeze is a signal to escalate.

**Limits.** Prometheus keeps two days of data in the local stack, so the Sloth period budget covers what is retained, not a full 30 days. The durable 30-day budgets on the ITOC Overview (purchase success and latency, from PostgreSQL) are authoritative for the weekly review. A cluster deployment should keep 30 days of retention, or use long-term storage, before relying on the gate's budget check. The firing-alert check does not depend on retention.

## Review

The on-call handoff reviews each budget weekly (see the [on-call policy](on-call.md)). Revisit this policy and the objectives each quarter, using the measured SLIs, incident history and how often the gate blocked or was overridden.

## Verification

`bash tests/ci/error-budget-gate.sh` runs the gate against a fake Prometheus query API and checks pass, firing-alert block, exhausted-budget block, unknown-budget notice, missing-rules block, idle-SLO notice, Prometheus-down block, the DEV advisory mode, override validation and URL validation. Running the gate against the live Compose Prometheus is recorded in [VERIFICATION.md](VERIFICATION.md).
