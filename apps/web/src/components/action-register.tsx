"use client";

import Link from "next/link";
import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { createColumnHelper, flexRender, getCoreRowModel, useReactTable } from "@tanstack/react-table";
import { ArrowUpRight, Search } from "lucide-react";
import { actions, type ActionRow, type ActionStatusFilter } from "@/lib/actions";
import { timestamp } from "@/lib/api";
import type { Environment } from "@/lib/contracts";
import { useEnvironment } from "./providers";
import { EmptyState, ErrorState, LoadingState, PageHeader, Panel } from "./common";
import { Badge } from "./ui/badge";
import { Button } from "./ui/button";
import { Input } from "./ui/input";
import { NativeSelect, NativeSelectOption } from "./ui/native-select";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "./ui/table";

const column = createColumnHelper<ActionRow>();

function ActionStatus({ value }: { value: ActionRow["status"] }) {
  const className = value === "Completed" ? "status-success" : value === "Blocked" ? "status-error" : value === "In Progress" ? "status-warning" : "status-neutral";
  return <Badge variant="outline" className={className}>{value}</Badge>;
}

function ActionList({ environment }: { environment: Environment }) {
  const [status, setStatus] = useState<ActionStatusFilter>("active");
  const [priority, setPriority] = useState<"" | ActionRow["priority"]>("");
  const [ownerInput, setOwnerInput] = useState("");
  const [searchInput, setSearchInput] = useState("");
  const [owner, setOwner] = useState("");
  const [search, setSearch] = useState("");
  const [cursors, setCursors] = useState([""]);
  const cursor = cursors[cursors.length - 1];
  useEffect(() => {
    const timer = window.setTimeout(() => {
      setOwner(ownerInput.trim());
      setSearch(searchInput.trim());
    }, 250);
    return () => window.clearTimeout(timer);
  }, [ownerInput, searchInput]);
  const query = useQuery({
    queryKey: ["corrective-actions", environment, status, priority, owner, search, cursor],
    queryFn: () => actions.list({ environment, status, priority, owner, search, cursor }),
    refetchOnWindowFocus: false,
  });
  const columns = useMemo(() => [
    column.accessor("title", { header: "Corrective action", cell: (info) => <Link className="table-link" href={`/incidents/${info.row.original.incident_id}#action-items`}>{info.getValue()}</Link> }),
    column.accessor("priority", { header: "Priority", cell: (info) => <Badge variant="outline" className={info.getValue() === "P1" ? "status-error" : info.getValue() === "P2" ? "status-warning" : "status-neutral"}>{info.getValue()}</Badge> }),
    column.accessor("status", { header: "Status", cell: (info) => <ActionStatus value={info.getValue()} /> }),
    column.accessor("owner", { header: "Owner" }),
    column.accessor("due_at", { header: "Due (WIB)", cell: (info) => {
      const due = info.getValue();
      if (!due) return <span className="muted">No date</span>;
      const overdue = info.row.original.status !== "Completed" && new Date(due).getTime() < Date.now();
      return <span className={overdue ? "error-text nowrap" : "nowrap"}>{timestamp(due)}{overdue ? " · Overdue" : ""}</span>;
    } }),
    column.accessor("incident_service", { header: "Service" }),
    column.accessor("incident_id", { header: "Incident", cell: (info) => <div className="table-stack"><Link className="table-link mono" href={`/incidents/${info.getValue()}#action-items`}>{info.getValue()}</Link><small>{info.row.original.incident_title}</small></div> }),
    column.display({ id: "review", header: "", cell: (info) => <Button size="sm" variant="ghost" asChild><Link href={`/incidents/${info.row.original.incident_id}#action-items`} aria-label={`Review ${info.row.original.title} in incident ${info.row.original.incident_id}`}>Review <ArrowUpRight data-icon="inline-end" /></Link></Button> }),
  ], []);
  // TanStack Table intentionally owns its mutable table instance.
  // eslint-disable-next-line react-hooks/incompatible-library
  const table = useReactTable({ data: query.data?.items ?? [], columns, getCoreRowModel: getCoreRowModel(), manualPagination: true });
  const reset = () => setCursors([""]);
  return <>
    <PageHeader title="RCA & Actions" description="Track corrective work across incidents in the selected environment. Changes are reviewed and audited in the source incident.">
      <Button variant="outline" asChild><Link href="/rca/reports">RCA reports</Link></Button>
      <Button variant="outline" onClick={() => void query.refetch()}>Refresh</Button>
    </PageHeader>
    <Panel title="Corrective-action register" description="Unfinished work first · earliest due date next · 25 per page. Legacy incident actions appear with normalized status and priority.">
      <div className="table-toolbar">
        <div className="search-field"><Search size={15} /><Input aria-label="Search actions or incidents" placeholder="Action, incident title or ID" maxLength={100} value={searchInput} onChange={(event) => { setSearchInput(event.target.value); reset(); }} /></div>
        <Input aria-label="Filter actions by owner" placeholder="Owner" maxLength={100} value={ownerInput} onChange={(event) => { setOwnerInput(event.target.value); reset(); }} className="action-owner-filter" />
        <NativeSelect aria-label="Action status" value={status} onChange={(event) => { setStatus(event.target.value as ActionStatusFilter); reset(); }}>
          <NativeSelectOption value="active">Active</NativeSelectOption><NativeSelectOption value="all">All statuses</NativeSelectOption>
          {(["Open", "In Progress", "Blocked", "Completed"] as const).map((value) => <NativeSelectOption key={value} value={value}>{value}</NativeSelectOption>)}
        </NativeSelect>
        <NativeSelect aria-label="Action priority" value={priority} onChange={(event) => { setPriority(event.target.value as "" | ActionRow["priority"]); reset(); }}>
          <NativeSelectOption value="">All priorities</NativeSelectOption>
          {(["P1", "P2", "P3"] as const).map((value) => <NativeSelectOption key={value} value={value}>{value}</NativeSelectOption>)}
        </NativeSelect>
        <Button variant="ghost" size="sm" onClick={() => { setStatus("active"); setPriority(""); setOwnerInput(""); setSearchInput(""); setOwner(""); setSearch(""); reset(); }}>Clear filters</Button>
      </div>
      {query.isPending ? <LoadingState /> : query.isError ? <div className="panel-padding"><ErrorState error={query.error} retry={() => void query.refetch()} /></div> : query.data.items.length === 0 ? <EmptyState title="No corrective actions in this view" description="Try another status, priority, owner or search term. Actions are added from an incident investigation." /> : <>
        <Table className="action-register-table"><TableHeader>{table.getHeaderGroups().map((group) => <TableRow key={group.id}>{group.headers.map((header) => <TableHead key={header.id}>{header.isPlaceholder ? null : flexRender(header.column.columnDef.header, header.getContext())}</TableHead>)}</TableRow>)}</TableHeader>
          <TableBody>{table.getRowModel().rows.map((row) => <TableRow key={`${row.original.incident_id}-${row.original.position}`}>{row.getVisibleCells().map((cell) => <TableCell key={cell.id}>{flexRender(cell.column.columnDef.cell, cell.getContext())}</TableCell>)}</TableRow>)}</TableBody></Table>
        <div className="pagination"><span>Page {cursors.length} · {query.data.items.length} actions shown</span><div><Button size="sm" variant="outline" disabled={cursors.length === 1} onClick={() => setCursors((values) => values.slice(0, -1))}>Previous</Button><Button size="sm" variant="outline" disabled={!query.data.more || !query.data.next} onClick={() => { if (query.data.next) setCursors((values) => [...values, query.data.next!]); }}>Next</Button></div></div>
      </>}
    </Panel>
  </>;
}

export function ActionRegister() {
  const { environment } = useEnvironment();
  return <ActionList key={environment} environment={environment} />;
}
