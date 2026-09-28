"use client";

import { useQuery } from "@tanstack/react-query";
import { incidentRecovery } from "@/lib/incident-recovery";
import { timestamp } from "@/lib/api";
import type { Incident } from "@/lib/incidents";
import { Button } from "./ui/button";
import { EmptyState, ErrorState, LoadingState, Panel } from "./common";
import { IncidentEditor } from "./incident-editor";

export function IncidentRecovery({ incident }: { incident: Incident }) {
  const query = useQuery({ queryKey: ["incident-recovery", incident.id, incident.version], queryFn: () => incidentRecovery.assess(incident.id), refetchOnWindowFocus: false });
  const a = query.data;
  const statusText = a?.status === "meets_target" ? "Both measured windows meet the 99.9% synthetic purchase target. Review other service and dependency evidence before resolving."
    : a?.status === "still_failing" ? "At least one window is below the target. Keep monitoring or investigate new failures."
    : a?.status === "insufficient_traffic" ? `Fewer than ${a.minimum_outcomes} completed purchases in at least one window. Recovery is unknown.`
    : a?.status === "collecting" ? "Monitoring has not covered two full five-minute windows yet. Recovery is unknown."
    : "";
  const snapshot = a && ["meets_target", "still_failing", "insufficient_traffic"].includes(a.status)
    ? `Synthetic purchase recovery assessment; incident=${incident.id}; environment=${incident.environment}; evaluated=${a.evaluated_at}; scope=purchase_path_environment; earlier=${a.earlier_start}..${a.split_at}, success=${a.earlier_success}, failed=${a.earlier_failed}; recent=${a.split_at}..${a.window_end}, success=${a.recent_success}, failed=${a.recent_failed}; target=${(a.target_success_rate * 100).toFixed(1)}%; minimum=${a.minimum_outcomes} outcomes per window; status=${a.status}. Completed synthetic purchases only; not proof of all customer recovery.`
    : "";
  return <section id="recovery"><Panel title="Recovery assessment" description="Two rolling five-minute windows of completed synthetic purchases after Monitoring began">
    {query.isPending ? <LoadingState /> : query.isError ? <div className="panel-padding"><ErrorState error={query.error} retry={() => void query.refetch()} /></div>
      : !a?.applicable ? <EmptyState title="No purchase-path assessment" description="This service is outside the synthetic purchase path. Validate recovery with service-specific measurements and evidence." />
      : a.status === "not_monitoring" ? <EmptyState title="Monitoring start is unavailable" description="No audited transition into Monitoring was found for this incident. Recovery remains unknown; review the lifecycle history and service-specific evidence." />
      : <div className="panel-padding incident-notes">
        <p><strong>{a.status === "meets_target" ? "Measured sample meets target" : a.status === "still_failing" ? "Failures remain" : "Recovery unknown"}</strong> · {statusText}</p>
        <p className="muted">Evaluated {timestamp(a.evaluated_at)} WIB · {incident.environment} · Monitoring since {a.monitoring_at ? `${timestamp(a.monitoring_at)} WIB` : "unknown"}. A terminal workflow is counted by completion time, once, in its purchase environment.</p>
        <div className="incident-impact-kpis">
          <div><small>Earlier 5 min · {timestamp(a.earlier_start)}–{timestamp(a.split_at)} WIB</small><strong>{a.earlier_success}/{a.earlier_total}</strong><span>{a.earlier_failed} failed</span></div>
          <div><small>Recent 5 min · {timestamp(a.split_at)}–{timestamp(a.window_end)} WIB</small><strong>{a.recent_success}/{a.recent_total}</strong><span>{a.recent_failed} failed</span></div>
        </div>
        <p className="muted">Each window needs {a.minimum_outcomes} completed purchases and at least {(a.target_success_rate * 100).toFixed(1)}% success. Low traffic is unknown, never healthy. Expected synthetic declines count as failures; unrelated customer traffic is outside this sample.</p>
        <div className="page-actions"><Button variant="outline" disabled={query.isFetching} onClick={() => void query.refetch()}>Refresh assessment</Button>{snapshot ? <IncidentEditor incident={incident} metricEvidence={{ kind: "metric", summary: snapshot }} /> : null}</div>
        {snapshot ? <p className="muted">Review assessment as evidence copies this timed sample into an audited incident draft. It does not resolve the incident.</p> : null}
      </div>}
  </Panel></section>;
}
