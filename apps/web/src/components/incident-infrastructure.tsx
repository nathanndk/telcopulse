"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { incidentInfrastructure } from "@/lib/incident-infrastructure";
import type { LogWindow } from "@/lib/incident-logs";
import { EmptyState, ErrorState, LoadingState, Panel } from "./common";
import { Button } from "./ui/button";
import { NativeSelect, NativeSelectOption } from "./ui/native-select";

function formatValue(value: number, unit: "count" | "bytes" | "nanocores") {
  if (unit === "bytes") return `${(value / 1024 / 1024).toFixed(1)} MiB`;
  if (unit === "nanocores") return `${(value / 1_000_000_000).toFixed(2)} cores`;
  return value.toFixed(0);
}

export function IncidentInfrastructure({ id }: { id: string }) {
  const [window, setWindow] = useState<LogWindow>("2h");
  const query = useQuery({ queryKey: ["incident-infrastructure", id, window], queryFn: () => incidentInfrastructure.list(id, window), refetchOnWindowFocus: false });
  return <Panel className="log-evidence-panel" title="Container and Kubernetes evidence" description="Datadog measurements for the incident environment and service. Pod metrics are correlation leads, not proof of cause." action={<div className="page-actions">
    <NativeSelect aria-label="Infrastructure evidence window" value={window} onChange={(event) => setWindow(event.target.value as LogWindow)}>
      <NativeSelectOption value="2h">Detection +2 hours</NativeSelectOption>
      <NativeSelectOption value="24h">Detection +24 hours</NativeSelectOption>
      <NativeSelectOption value="72h">Detection +72 hours</NativeSelectOption>
    </NativeSelect>
    <Button size="sm" variant="outline" onClick={() => void query.refetch()} disabled={query.isFetching}>Refresh</Button>
  </div>}>
    {query.isPending ? <LoadingState /> : query.isError ? <div className="panel-padding"><ErrorState error={query.error} retry={() => void query.refetch()} /></div> : !query.data.configured ? <EmptyState title="Datadog is not configured" description="Connect a read-only Datadog metrics token and Kubernetes telemetry to inspect pod restarts, memory and CPU near detection." /> : query.data.series.length === 0 || query.data.series.every((series) => series.points.length === 0) ? <EmptyState title="No matching container measurements" description="Check Datadog collection, env/service tags, Kubernetes namespace, retention and the selected incident window." /> : <div className="panel-padding">
      <p className="muted">Datadog · {query.data.window_start} to {query.data.window_end} · pod-tagged measurements</p>
      {query.data.limited ? <p className="muted">Showing the first four pod series per measurement. Open Datadog for the full fleet.</p> : null}
      <div className="incident-infrastructure-list">{query.data.series.map((series) => {
        const first = series.points[0];
        const last = series.points.at(-1);
        return <article className="incident-infrastructure-row" key={`${series.key}:${series.namespace}:${series.pod}`}>
          <div><strong>{series.label}</strong><p className="muted mono">{series.namespace} / {series.pod}</p></div>
          <div className="incident-infrastructure-reading"><strong>{last ? formatValue(last.value, series.unit) : "No data"}</strong><span className="muted">{first && last ? `${formatValue(first.value, series.unit)} at start · ${series.points.length} samples` : "No usable samples"}</span></div>
        </article>;
      })}</div>
    </div>}
  </Panel>;
}
