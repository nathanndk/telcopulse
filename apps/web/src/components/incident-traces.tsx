"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { incidentTraces } from "@/lib/incident-traces";
import type { LogWindow } from "@/lib/incident-logs";
import { EmptyState, ErrorState, LoadingState, Panel } from "./common";
import { Badge } from "./ui/badge";
import { Button } from "./ui/button";
import { NativeSelect, NativeSelectOption } from "./ui/native-select";

export function IncidentTraces({ id, viewer }: { id: string; viewer: string | null }) {
  const [window, setWindow] = useState<LogWindow>("24h");
  const query = useQuery({
    queryKey: ["incident-traces", id, window],
    queryFn: () => incidentTraces.list(id, window),
    refetchOnWindowFocus: false,
  });
  return <Panel className="log-evidence-panel" title="Trace and dependency evidence" description="Observed spans for the affected service near detection. A slow or failed span is a lead, not a root-cause conclusion." action={<div className="page-actions">
    <NativeSelect aria-label="Trace evidence window" value={window} onChange={(event) => setWindow(event.target.value as LogWindow)}>
      <NativeSelectOption value="2h">Detection +2 hours</NativeSelectOption>
      <NativeSelectOption value="24h">Detection +24 hours</NativeSelectOption>
      <NativeSelectOption value="72h">Detection +72 hours</NativeSelectOption>
    </NativeSelect>
    <Button size="sm" variant="outline" onClick={() => void query.refetch()} disabled={query.isFetching}>Refresh</Button>
  </div>}>
    {query.isPending ? <LoadingState /> : query.isError ? <div className="panel-padding"><ErrorState error={query.error} retry={() => void query.refetch()} /></div> : !query.data.configured ? <EmptyState title="Trace search is not configured" description="Connect the read-only Jaeger query endpoint to show incident-period service and dependency spans." /> : query.data.items.length === 0 ? <EmptyState title="No matching traces in this window" description="Check trace retention and sampling. An empty query is not proof that the service had no failures." /> : <div className="panel-padding incident-trace-list">
      <p className="muted">Jaeger · {query.data.window_start} to {query.data.window_end} · up to four traces with verified environment context</p>
      {query.data.items.map((trace) => <article className="incident-trace" key={trace.trace_id}>
        <div className="incident-trace-heading">
          <div><strong>{new Date(trace.started_at).toLocaleString("en-GB", { timeZone: "Asia/Jakarta" })} WIB</strong><p className="mono muted">{trace.trace_id}</p></div>
          {viewer ? <Button size="sm" variant="outline" asChild><a href={`${viewer}/trace/${trace.trace_id}`} target="_blank" rel="noopener noreferrer">Open trace ↗</a></Button> : null}
        </div>
        <p className="muted">Observed calls: {trace.edges.length ? trace.edges.map((edge) => `${edge.from} → ${edge.to}`).join(" · ") : "No cross-service or database edge recorded"}</p>
        <div className="incident-trace-nodes">{trace.nodes.map((node) => <div key={`${node.kind}:${node.name}`}>
          <span>{node.name}</span><Badge variant="outline" className={node.error ? "status-error" : "status-neutral"}>{node.error ? "Error observed" : node.kind}</Badge><span className="muted">Max span {node.max_duration_ms.toFixed(1)} ms</span>
        </div>)}</div>
      </article>)}
    </div>}
  </Panel>;
}
