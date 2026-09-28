"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { incidentLogs, type LogWindow } from "@/lib/incident-logs";
import { EmptyState, ErrorState, LoadingState, Panel } from "./common";
import { Badge } from "./ui/badge";
import { Button } from "./ui/button";
import { NativeSelect, NativeSelectOption } from "./ui/native-select";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "./ui/table";

export function IncidentLogs({ id }: { id: string }) {
  const [window, setWindow] = useState<LogWindow>("24h");
  const query = useQuery({
    queryKey: ["incident-logs", id, window],
    queryFn: () => incidentLogs.list(id, window),
    refetchOnWindowFocus: false,
  });
  return <Panel className="log-evidence-panel" title="Logs evidence" description="Indexed structured logs for the affected service, from 15 minutes before detection. This is source evidence, not a recorded incident conclusion." action={<div className="page-actions">
    <NativeSelect aria-label="Log evidence window" value={window} onChange={(event) => setWindow(event.target.value as LogWindow)}>
      <NativeSelectOption value="2h">Detection +2 hours</NativeSelectOption>
      <NativeSelectOption value="24h">Detection +24 hours</NativeSelectOption>
      <NativeSelectOption value="72h">Detection +72 hours</NativeSelectOption>
    </NativeSelect>
    <Button size="sm" variant="outline" onClick={() => void query.refetch()} disabled={query.isFetching}>Refresh</Button>
  </div>}>
    {query.isPending ? <LoadingState /> : query.isError ? <div className="panel-padding"><ErrorState error={query.error} retry={() => void query.refetch()} /></div> : !query.data.configured ? <EmptyState title="Splunk log search is not configured" description="Connect a read-only Splunk Search API credential to show indexed incident-period logs. No log result is inferred from transactions or metrics." /> : query.data.items.length === 0 ? <EmptyState title="No indexed logs in this window" description="Check the selected detection window, source retention and ingestion health before interpreting this as no service errors." /> : <>
      <p className="log-evidence-window">Splunk · {query.data.window_start} to {query.data.window_end} · {query.data.items.length} matching events</p>
      <Table className="log-evidence-table">
        <TableHeader><TableRow><TableHead>Time (source)</TableHead><TableHead>Level</TableHead><TableHead>Service</TableHead><TableHead>Message</TableHead><TableHead>Trace ID</TableHead></TableRow></TableHeader>
        <TableBody>{query.data.items.map((item, index) => <TableRow key={`${item.at}-${item.trace_id}-${index}`}>
          <TableCell className="nowrap">{item.at || "Unknown"}</TableCell>
          <TableCell><Badge variant="outline" className={item.level === "ERROR" ? "status-error" : item.level === "WARN" ? "status-warning" : "status-neutral"}>{item.level}</Badge></TableCell>
          <TableCell>{item.service}</TableCell>
          <TableCell className="log-evidence-message">{item.message || "No message"}{item.error_code ? <small>Error: {item.error_code}</small> : null}{item.transaction_id ? <small>Transaction: {item.transaction_id}</small> : null}</TableCell>
          <TableCell className="mono">{item.trace_id || "Not recorded"}</TableCell>
        </TableRow>)}</TableBody>
      </Table>
    </>}
  </Panel>;
}
