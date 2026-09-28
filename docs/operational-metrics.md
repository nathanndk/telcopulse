# Operational response metrics

The ITOC Overview shows measured operational response for the selected development or staging environment. Incident-service owns the incident measures; deployment-service owns the deployment measure. Each returns an independently computed rolling 30-day window and a null duration/rate when no eligible sample exists. If one service fails, the panel shows its error and retains the other service's measurements rather than displaying a fabricated zero.

Incident measures use incidents whose immutable `detected_at` falls in the window:

| Measure | Definition |
| --- | --- |
| Incident count | All detected incidents in the environment, including now-resolved incidents. |
| SEV-1 count | Those incidents with current SEV-1 severity; severity can change after detection. |
| MTTA | Mean seconds from detection to the first recorded acknowledgement. Unacknowledged incidents are excluded and the sample count is shown. |
| MTTR | Mean seconds from detection to the current recorded resolution. Unresolved and reopened incidents are excluded and the sample count is shown. |
| Repeat candidates | Subsequent incidents in the window with the same service and case-folded, trimmed root cause of at least five characters. Unknown root causes are excluded. This is a triage heuristic, not proven common causation. |

**Escalations** count immutable `escalated` audit events whose event time falls in the window, including escalations of incidents detected earlier. Multiple escalations of one incident count separately. A missing acknowledgement or resolution produces a null average, not zero seconds.

**Deployment failure rate** uses reported deployments whose first `Completed` or `Failed` event falls in the window. Pending/Running-only releases are excluded from the denominator. A deployment counts as failed when its current status is `Failed` or `Rolled Back`; a later rollback changes the current rate for its original cohort. The response includes evaluated and failed counts, and its rate is null when no release reached a terminal result. Synthetic bad-deployment markers participate in this local metric, so it is not a measure of real GitLab/Jenkins delivery until those systems report verified events.

The public read APIs are `GET /api/v1/incidents/operations?environment=development|staging` and `GET /api/v1/deployments/operations?environment=development|staging`. The deployment service also accepts its separately supported `production` event scope, but the current ITOC console and incident data are limited to development/staging. These metrics are refreshed through the console's usual query cycle; they are not alert rules or external paging signals.
