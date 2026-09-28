"use client";

import { useState } from "react";
import Link from "next/link";
import { useQuery } from "@tanstack/react-query";
import { incidentImpact } from "@/lib/incident-impact";
import type { LogWindow } from "@/lib/incident-logs";
import { timestamp } from "@/lib/api";
import { EmptyState, ErrorState, LoadingState, Panel } from "./common";
import { Button } from "./ui/button";
import { NativeSelect, NativeSelectOption } from "./ui/native-select";
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from "./ui/table";

export function IncidentImpact({ id }: { id: string }) {
  const [window, setWindow] = useState<LogWindow>("2h");
  const [cursors, setCursors] = useState([""]);
  const cursor = cursors[cursors.length - 1];
  const query = useQuery({ queryKey: ["incident-purchase-impact", id, window, cursor], queryFn: () => incidentImpact.list(id, window, cursor) });
  return <Panel className="log-evidence-panel" title="Observed purchase impact" description="Recorded synthetic purchase outcomes in the incident environment; correlation does not prove incident causation or replace assessed impact." action={<div className="page-actions">
    <NativeSelect aria-label="Purchase impact window" value={window} onChange={(event) => { setWindow(event.target.value as LogWindow); setCursors([""]); }}>
      <NativeSelectOption value="2h">Detection +2 hours</NativeSelectOption>
      <NativeSelectOption value="24h">Detection +24 hours</NativeSelectOption>
      <NativeSelectOption value="72h">Detection +72 hours</NativeSelectOption>
    </NativeSelect>
    <Button size="sm" variant="outline" onClick={() => { setCursors([""]); void query.refetch(); }} disabled={query.isFetching}>Refresh</Button>
  </div>}>
    {query.isPending ? <LoadingState /> : query.isError ? <div className="panel-padding"><ErrorState error={query.error} retry={() => void query.refetch()} /></div> : !query.data.applicable ? <EmptyState title="No purchase-path correlation for this service" description="This incident's service is outside the synthetic purchase workflow. Use its logs, traces and metrics for service-specific evidence." /> : <div className="panel-padding">
      <p className="muted incident-metric-note">Completed purchases from {timestamp(query.data.window_start)} to {timestamp(query.data.window_end)} WIB, starting 15 minutes before detection. Counts cover the whole selected period; the table shows failed outcomes only.</p>
      <div className="incident-impact-kpis">
        <div><small>Completed purchases</small><strong>{query.data.total}</strong></div>
        <div><small>Successful</small><strong>{query.data.success}</strong></div>
        <div><small>Failed</small><strong>{query.data.failed}</strong></div>
        <div><small>Customers with a failed purchase</small><strong>{query.data.failed_customers}</strong></div>
      </div>
      {query.data.total === 0 ? <p className="muted">No completed purchases were recorded in this period. This is not evidence that customers were unaffected.</p> : query.data.failed === 0 ? <p className="muted">No failed purchases were recorded in this period.</p> : <>
        <Table aria-label="Observed failed purchases"><TableHeader><TableRow><TableHead>Transaction</TableHead><TableHead>Customer ID</TableHead><TableHead>Error code</TableHead><TableHead>Time (WIB)</TableHead><TableHead>Trace ID</TableHead></TableRow></TableHeader><TableBody>{query.data.items.map((item) => <TableRow key={item.id}><TableCell><Link className="table-link mono" href={`/transactions/${item.id}`}>{item.id}</Link></TableCell><TableCell className="mono">{item.customer_id}</TableCell><TableCell>{item.error_code || "Unclassified"}</TableCell><TableCell>{timestamp(item.created_at)}</TableCell><TableCell className="mono">{item.trace_id}</TableCell></TableRow>)}</TableBody></Table>
        <div className="pagination"><span>Failed purchase page {cursors.length} · newest first</span><div><Button variant="outline" disabled={cursors.length === 1 || query.isFetching} onClick={() => setCursors((value) => value.slice(0, -1))}>Previous</Button><Button variant="outline" disabled={!query.data.more || !query.data.next || query.isFetching} onClick={() => setCursors((value) => [...value, query.data!.next!])}>Next</Button></div></div>
      </>}
      <p className="muted incident-metric-note">Distinct customer IDs count only observed failed purchases. Other customer impact may exist, and these outcomes may have unrelated causes.</p>
    </div>}
  </Panel>;
}
