import { PageHeader, Panel } from "@/components/common";
import { UserManagement } from "@/components/user-management";
import { Badge } from "@/components/ui/badge";
export const metadata = { title: "Settings" };
export default function Page() {
  return (
    <>
      <PageHeader
        title="Workspace settings"
        description="Local access controls and runtime integration status."
      />
      <UserManagement />
      <Panel
        title="Environment configuration"
        description="Runtime configuration is managed through Compose and environment variables."
      >
        <dl className="details-list">
          <dt>Access mode</dt>
          <dd>
            <Badge variant="outline">Local development</Badge>
          </dd>
          <dt>API</dt>
          <dd>Go gateway and domain services · same-origin /api/v1 proxy</dd>
          <dt>Persistence</dt>
          <dd>PostgreSQL · durable transactions, incidents, deployments and audit history</dd>
          <dt>Customer data</dt>
          <dd>Fictional subscribers · MSISDN masked in responses and logs</dd>
          <dt>Payment processing</dt>
          <dd>Synthetic adapter · no external payment charges</dd>
          <dt>Authentication</dt>
          <dd>
            Local sessions and server-enforced roles are opt-in. SSO is not configured.
          </dd>
        </dl>
      </Panel>
      <Panel
        title="Observability integrations"
        description="Local telemetry is active. External vendor export requires separate configuration and verification."
      >
        <dl className="details-list">
          <dt>Prometheus / Grafana</dt>
          <dd>Local metrics collection, alert rules and operational dashboards</dd>
          <dt>Jaeger</dt>
          <dd>Local distributed trace storage and investigation</dd>
          <dt>Splunk</dt>
          <dd>Optional HEC export · external indexing unverified</dd>
          <dt>Dynatrace</dt>
          <dd>Optional authenticated OTLP export · external ingestion unverified</dd>
          <dt>Datadog</dt>
          <dd>Not configured</dd>
        </dl>
      </Panel>
    </>
  );
}
