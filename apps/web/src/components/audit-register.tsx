"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Search } from "lucide-react";
import { audit, auditResourcePath, type AuditSource } from "@/lib/audit";
import { timestamp } from "@/lib/api";
import type { Environment } from "@/lib/contracts";
import { useEnvironment } from "./providers";
import { EmptyState, ErrorState, LoadingState, PageHeader, Panel } from "./common";
import { Badge } from "./ui/badge";
import { Button } from "./ui/button";
import { Input } from "./ui/input";
import { NativeSelect, NativeSelectOption } from "./ui/native-select";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "./ui/table";

function AuditList({ environment }: { environment: Environment }) {
  const [source, setSource] = useState<AuditSource>("");
  const [actorInput, setActorInput] = useState("");
  const [actionInput, setActionInput] = useState("");
  const [resourceInput, setResourceInput] = useState("");
  const [search, setSearch] = useState({ actor: "", action: "", resource: "" });
  const [cursors, setCursors] = useState([""]);
  const cursor = cursors[cursors.length - 1];
  useEffect(() => {
    const timer = window.setTimeout(() => setSearch({ actor: actorInput.trim(), action: actionInput.trim(), resource: resourceInput.trim() }), 250);
    return () => window.clearTimeout(timer);
  }, [actorInput, actionInput, resourceInput]);
  const query = useQuery({
    queryKey: ["operations-audit", environment, source, search, cursor],
    queryFn: () => audit.list({ environment, source, ...search, cursor }),
    refetchOnWindowFocus: false,
  });
  const reset = () => setCursors([""]);
  return <>
    <PageHeader title="Audit Log" description="A chronological register of recorded operator changes in the selected environment.">
      <Button variant="outline" onClick={() => void query.refetch()}>Refresh</Button>
    </PageHeader>
    <Panel title="Operations audit" description="Incident lifecycle, simulation controls and deployment status events · newest first · 25 per page. Authentication and transaction events are outside this register.">
      <div className="table-toolbar">
        <NativeSelect aria-label="Audit source" value={source} onChange={(event) => { setSource(event.target.value as AuditSource); reset(); }}>
          <NativeSelectOption value="">All sources</NativeSelectOption>
          <NativeSelectOption value="incident">Incidents</NativeSelectOption>
          <NativeSelectOption value="simulation">Simulations</NativeSelectOption>
          <NativeSelectOption value="deployment">Deployments</NativeSelectOption>
        </NativeSelect>
        <div className="search-field"><Search size={15} /><Input aria-label="Filter audit by actor" placeholder="Actor" maxLength={100} value={actorInput} onChange={(event) => { setActorInput(event.target.value); reset(); }} /></div>
        <Input aria-label="Filter audit by action" placeholder="Action" maxLength={80} value={actionInput} onChange={(event) => { setActionInput(event.target.value); reset(); }} />
        <Input aria-label="Filter audit by resource ID" placeholder="Exact resource ID" maxLength={120} value={resourceInput} onChange={(event) => { setResourceInput(event.target.value); reset(); }} />
        <Button variant="ghost" size="sm" onClick={() => { setSource(""); setActorInput(""); setActionInput(""); setResourceInput(""); setSearch({ actor: "", action: "", resource: "" }); reset(); }}>Clear filters</Button>
      </div>
      {query.isPending ? <LoadingState /> : query.isError ? <div className="panel-padding"><ErrorState error={query.error} retry={() => void query.refetch()} /></div> : query.data.items.length === 0 ? <EmptyState title="No audit events in this view" description="Try another source, actor, action or resource. New operations appear here after they are recorded." /> : <>
        <Table><TableHeader><TableRow><TableHead>Recorded (WIB)</TableHead><TableHead>Source</TableHead><TableHead>Action</TableHead><TableHead>Actor</TableHead><TableHead>Resource</TableHead><TableHead>Detail</TableHead></TableRow></TableHeader>
          <TableBody>{query.data.items.map((event) => <TableRow key={event.id}>
            <TableCell className="nowrap">{timestamp(event.at)}</TableCell>
            <TableCell><Badge variant="outline" className="status-neutral">{event.source}</Badge></TableCell>
            <TableCell>{event.action}</TableCell>
            <TableCell>{event.actor || "System"}</TableCell>
            <TableCell><Link className="table-link mono" href={auditResourcePath(event)}>{event.resource_id}</Link></TableCell>
            <TableCell>{event.changes.length > 0 ? <div className="table-stack">{event.changes.map((change) => <small key={change.field}>{change.field.replaceAll("_", " ")}{change.before !== undefined || change.after !== undefined ? `: ${change.before || "—"} → ${change.after || "—"}` : " changed"}</small>)}</div> : event.note || "—"}</TableCell>
          </TableRow>)}</TableBody></Table>
        <div className="pagination"><span>Page {cursors.length} · {query.data.items.length} events shown</span><div><Button size="sm" variant="outline" disabled={cursors.length === 1} onClick={() => setCursors((values) => values.slice(0, -1))}>Previous</Button><Button size="sm" variant="outline" disabled={!query.data.more || !query.data.next} onClick={() => { const next = query.data.next; if (next) setCursors((values) => [...values, next]); }}>Next</Button></div></div>
      </>}
    </Panel>
  </>;
}

export function AuditRegister() {
  const { environment } = useEnvironment();
  return <AuditList key={environment} environment={environment} />;
}
