"use client";

import { useQuery } from "@tanstack/react-query";
import { operations } from "@/lib/operations";
import type { Environment } from "@/lib/contracts";
import { Panel, ErrorState } from "./common";

function responseTime(seconds: number | null | undefined) {
  if (seconds == null) return "—";
  if (seconds < 1) return `${seconds.toFixed(2)} s`;
  if (seconds < 60) return `${seconds.toFixed(1)} s`;
  if (seconds < 3600) return `${(seconds / 60).toFixed(1)} min`;
  return `${(seconds / 3600).toFixed(1)} h`;
}

export function OperationalMetrics({ environment }: { environment: Environment }) {
  const incidents = useQuery({
    queryKey: ["incidents", "operations", environment],
    queryFn: () => operations.incidents(environment),
  });
  const deployments = useQuery({
    queryKey: ["deployments", "operations", environment],
    queryFn: () => operations.deployments(environment),
  });
  const incidentMetrics = incidents.data;
  const deploymentMetrics = deployments.data;
  const metrics = [
    { label: "Incidents", value: incidentMetrics?.incident_count, note: "Detected in window" },
    { label: "SEV-1", value: incidentMetrics?.sev1_count, note: "Detected in window" },
    { label: "MTTA", value: responseTime(incidentMetrics?.mtta_seconds), note: `${incidentMetrics?.acknowledged_count ?? "—"} acknowledged samples` },
    { label: "MTTR", value: responseTime(incidentMetrics?.mttr_seconds), note: `${incidentMetrics?.resolved_count ?? "—"} currently resolved samples` },
    { label: "Escalations", value: incidentMetrics?.escalation_events, note: "Audit events in window" },
    { label: "Repeat candidates", value: incidentMetrics?.repeat_count, note: "Same service + root cause" },
    { label: "Deployment failure rate", value: deploymentMetrics?.failure_rate_percent == null ? "—" : `${deploymentMetrics.failure_rate_percent.toFixed(1)}%`, note: `${deploymentMetrics?.failed ?? "—"} failed / ${deploymentMetrics?.evaluated ?? "—"} evaluated` },
  ];
  return (
    <Panel title="Operational response" description="Rolling 30 days · selected environment · recorded incidents and reported deployments">
      <dl className="operations-grid">
        {metrics.map((metric) => (
          <div key={metric.label}>
            <dt>{metric.label}</dt>
            <dd>{metric.value ?? "—"}</dd>
            <small>{metric.note}</small>
          </div>
        ))}
      </dl>
      {incidents.isError && <div className="panel-padding"><ErrorState error={incidents.error} retry={() => incidents.refetch()} /></div>}
      {deployments.isError && <div className="panel-padding"><ErrorState error={deployments.error} retry={() => deployments.refetch()} /></div>}
      <p className="panel-note">
        MTTA/MTTR use incidents detected in this window and only include recorded
        acknowledgements/current resolutions. Repeat candidates are a same-service,
        same-root-cause heuristic. Deployment rate uses releases reaching Completed
        or Failed; later rollbacks count as failures. “—” means no sample or source data.
        Local synthetic and test activity is included.
      </p>
    </Panel>
  );
}
