# Deployment events and incident correlation

TelcoPulse stores **reported** delivery events from a local CI-style caller. It does not infer a deployment from an image tag or claim that Kubernetes pods changed. The new `deployment-service` owns this data in PostgreSQL; the gateway exposes read-only operator queries at `GET /api/v1/deployments` and `GET /api/v1/deployments/{id}`. The web console shows recent activity and an immutable event timeline. Incident investigation searches the same service and environment from two hours before detection through thirty minutes afterward. A time match is evidence to inspect, not automatic root cause.

The internal write endpoint is `POST /internal/deployments/events` on `deployment-service`. It is reachable only on the Compose internal network and requires the existing `SERVICE_TOKEN` bearer credential. This local-mode credential is service-to-service authentication, not a replacement for human SSO/RBAC or a production CI ingress. Event IDs and deployment IDs are caller-assigned stable identifiers; retry a lost response with the **same event ID and identical payload**. Identical replay returns 200; a new event returns 201; conflicting replay or invalid lifecycle returns 409. Invalid payloads return 400 or 422.

Example payload (fictional):

```json
{
  "event_id": "gitlab-pipeline-4812-completed",
  "deployment_id": "gitlab-pipeline-4812",
  "service": "payment-service",
  "version": "1.4.0",
  "commit_sha": "abcdef0123456789abcdef0123456789abcdef01",
  "environment": "staging",
  "deployer": "ci:gitlab",
  "status": "Completed",
  "occurred_at": "2026-09-27T11:45:00Z"
}
```

Allowed transitions are Pending → Running, Completed or Failed; Running → Completed or Failed; Completed → Rolled Back. Failed and Rolled Back are terminal. A caller may first report Pending, Running, Completed or Failed when earlier events were not available. Subsequent events must have a later timestamp and the same deployment ID, service, environment, version and commit. `deployer` on the record preserves the first actor; every audit event stores the actor who reported that transition, allowing a different rollback operator.

List queries accept `environment=development|staging|production`, optional exact `service`, RFC3339 `since`/`until` (at most thirty days apart), and `limit` from 1 to 100. Defaults are the development environment, last 24 hours and 50 records. A record is included when **any** event falls in the window, even if a later rollback changed its current status after the window. The detail API returns current state and ordered immutable events.

For the local demonstration, run `WEB_PORT=3001 python3 tests/integration/deployments.py` against a started Compose stack. It sends synthetic Pending, Running, Completed and Rolled Back events from inside the authenticated service container, then verifies replay, conflicts, actor history and gateway time-window correlation. The separate [bad-deployment simulation](simulations.md#synthetic-bad-deployment) reports a synthetic marker and rollback tied to actual purchase failures. Neither path contacts GitLab, Jenkins or a Kubernetes cluster; production CI ingestion and deployment markers in observability vendors remain planned.
