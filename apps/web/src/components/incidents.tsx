"use client";
import { IncidentCreator } from "./incident-creator";
import { IncidentTelemetry } from "./incident-telemetry";
import { IncidentDeployments } from "./deployments";
import { IncidentEditor } from "./incident-editor";
import { IncidentEscalation } from "./incident-escalation";
import { IncidentPostmortem } from "./incident-postmortem";
import { IncidentLogs } from "./incident-logs";
import { IncidentTraces } from "./incident-traces";
import { IncidentMetrics } from "./incident-metrics";
import { IncidentInfrastructure } from "./incident-infrastructure";
import { IncidentImpact } from "./incident-impact";
import { IncidentRecovery } from "./incident-recovery";
import { useEffect, useState } from "react";
import { Search } from "lucide-react";
import Link from "next/link";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { flexRender, getCoreRowModel, useReactTable, type ColumnDef, type VisibilityState } from "@tanstack/react-table";
import { incidents, incidentStates, incidentColumns, savedViews, type IncidentSummary, type SavedViewFilters, type IncidentListFilters } from "@/lib/incidents";
import { timestamp } from "@/lib/api";
import { useEnvironment, usePermission } from "./providers";
import {
  PageHeader,
  Panel,
  EmptyState,
  ErrorState,
  LoadingState,
} from "./common";
import { Button } from "./ui/button";
import { Input } from "./ui/input";
import { Badge } from "./ui/badge";
import { NativeSelect, NativeSelectOption } from "./ui/native-select";
import {
  Table,
  TableHeader,
  TableBody,
  TableRow,
  TableHead,
  TableCell,
} from "./ui/table";
import type { Environment } from "@/lib/contracts";

function Severity({ value }: { value: string }) {
  return (
    <Badge
      variant="outline"
      className={
        value === "SEV-1" || value === "SEV-2"
          ? "status-error"
          : "status-neutral"
      }
    >
      {value}
    </Badge>
  );
}
const registerColumns: ColumnDef<IncidentSummary>[] = [
  { id: "incident", header: "Incident", cell: ({ row }) => <><Link className="table-link" href={`/incidents/${row.original.id}`}>{row.original.title}</Link><div className="muted mono">{row.original.id}</div></> },
  { id: "severity", header: "Severity", cell: ({ row }) => <Severity value={row.original.severity} /> },
  { id: "state", header: "State", cell: ({ row }) => row.original.state },
  { id: "service", header: "Affected service", cell: ({ row }) => row.original.service },
  { id: "owner", header: "Owner", cell: ({ row }) => row.original.owner || "Unassigned" },
  { id: "owning_team", header: "Owning team", cell: ({ row }) => <>{row.original.owning_team || "Unassigned"}{row.original.escalation_level > 0 ? ` · L${row.original.escalation_level}` : ""}</> },
  { id: "detected_at", header: "Opened (WIB)", cell: ({ row }) => timestamp(row.original.detected_at) },
  { id: "updated_at", header: "Last update (WIB)", cell: ({ row }) => timestamp(row.original.updated_at) },
  { id: "actions", header: "Actions", cell: ({ row }) => <Link className="table-link" href={`/incidents/${row.original.id}`}>View details</Link> },
];
function IncidentRegisterTable({ items, visibility, onVisibilityChange }: { items: IncidentSummary[]; visibility: VisibilityState; onVisibilityChange: (value: VisibilityState) => void }) {
  // TanStack owns this mutable table instance; it is not passed into a memoized component.
  // eslint-disable-next-line react-hooks/incompatible-library
  const table = useReactTable({ data: items, columns: registerColumns, state: { columnVisibility: visibility }, onColumnVisibilityChange: (updater) => onVisibilityChange(typeof updater === "function" ? updater(visibility) : updater), getCoreRowModel: getCoreRowModel(), manualSorting: true, manualPagination: true });
  return <div className="incident-register-table"><div className="incident-scroll-hint">Scroll the table sideways for more columns</div><Table aria-label="Incident register"><TableHeader>{table.getHeaderGroups().map((group) => <TableRow key={group.id}>{group.headers.map((header) => <TableHead key={header.id}>{header.isPlaceholder ? null : flexRender(header.column.columnDef.header, header.getContext())}</TableHead>)}</TableRow>)}</TableHeader><TableBody>{table.getRowModel().rows.map((row) => <TableRow key={row.id}>{row.getVisibleCells().map((cell) => <TableCell key={cell.id} className={cell.column.id === "detected_at" || cell.column.id === "updated_at" ? "nowrap" : undefined}>{flexRender(cell.column.columnDef.cell, cell.getContext())}</TableCell>)}</TableRow>)}</TableBody></Table></div>;
}
function rangeSince(value: string) {
  const hours = value === "24h" ? 24 : value === "7d" ? 24 * 7 : value === "30d" ? 24 * 30 : 0;
  return hours ? new Date(Date.now() - hours * 60 * 60 * 1000).toISOString() : "";
}
export function IncidentList() {
  const { environment } = useEnvironment();
  const canCreate = usePermission("createIncident");
  return (
    <>
      <div className="table-toolbar">
        {canCreate ? <IncidentCreator environment={environment} /> : null}
        <span className="muted">{canCreate ? "Record an operator-detected incident" : "Your role can review incidents but cannot create them"}</span>
      </div>
      <FilteredList key={environment} environment={environment} />
    </>
  );
}
function FilteredList({ environment }: { environment: Environment }) {
  const queryClient = useQueryClient();
  const [state, setState] = useState("");
  const [severity, setSeverity] = useState<IncidentListFilters["severity"]>("");
  const [serviceInput, setServiceInput] = useState("");
  const [ownerInput, setOwnerInput] = useState("");
  const [searchInput, setSearchInput] = useState("");
  const [service, setService] = useState("");
  const [owner, setOwner] = useState("");
  const [search, setSearch] = useState("");
  const [range, setRange] = useState("all");
  const [since, setSince] = useState("");
  const [sort, setSort] = useState<IncidentListFilters["sort"]>("updated_desc");
  const [columnVisibility, setColumnVisibility] = useState<VisibilityState>({});
  const [cursors, setCursors] = useState<string[]>([""]);
  const [selectedView, setSelectedView] = useState("");
  const [viewName, setViewName] = useState("");
  const [viewError, setViewError] = useState("");
  const [viewNotice, setViewNotice] = useState("");
  const views = useQuery({ queryKey: ["incident-saved-views", environment], queryFn: () => savedViews.list(environment) });
  const saveView = useMutation({
    mutationFn: ({ id, name, filters }: { id: string; name: string; filters: SavedViewFilters }) =>
      id ? savedViews.update(id, environment, name, filters) : savedViews.create(environment, name, filters),
    onSuccess: (saved, variables) => {
      setSelectedView(saved.id);
      setViewName(saved.name);
      setViewError("");
      setViewNotice(variables.id ? "View updated" : "View saved");
      void queryClient.invalidateQueries({ queryKey: ["incident-saved-views", environment] });
    },
    onError: (error) => { setViewNotice(""); setViewError(error instanceof Error ? error.message : "Could not save view"); },
  });
  const deleteView = useMutation({
    mutationFn: savedViews.delete,
    onSuccess: () => {
      setSelectedView("");
      setViewName("");
      setViewError("");
      setViewNotice("View deleted");
      void queryClient.invalidateQueries({ queryKey: ["incident-saved-views", environment] });
    },
    onError: (error) => { setViewNotice(""); setViewError(error instanceof Error ? error.message : "Could not delete view"); },
  });
  const cursor = cursors[cursors.length - 1];
  useEffect(() => {
    const timer = window.setTimeout(() => {
      setService(serviceInput.trim());
      setOwner(ownerInput.trim());
      setSearch(searchInput.trim());
    }, 250);
    return () => window.clearTimeout(timer);
  }, [serviceInput, ownerInput, searchInput]);
  const reset = () => setCursors([""]);
  const query = useQuery({
    queryKey: ["incidents", environment, state, severity, service, owner, search, since, sort, cursor],
    queryFn: () => incidents.list({ environment, state, severity, service, owner, search, since, sort, cursor }),
  });
  const currentFilters: SavedViewFilters = { state, severity, service: serviceInput.trim(), owner: ownerInput.trim(), search: searchInput.trim(), range: range as SavedViewFilters["range"], sort, columns: incidentColumns.filter((column) => columnVisibility[column] !== false) };
  const selectView = (id: string) => {
    setSelectedView(id);
    const view = views.data?.find((candidate) => candidate.id === id);
    if (!view) { setViewName(""); return; }
    const f = view.filters;
    setViewName(view.name);
    setState(f.state);
    setSeverity(f.severity as IncidentListFilters["severity"]);
    setServiceInput(f.service); setService(f.service);
    setOwnerInput(f.owner); setOwner(f.owner);
    setSearchInput(f.search); setSearch(f.search);
    setRange(f.range); setSince(rangeSince(f.range));
    setSort(f.sort);
    setColumnVisibility(Object.fromEntries(incidentColumns.map((column) => [column, f.columns.includes(column)])));
    setCursors([""]);
    setViewError("");
    setViewNotice("");
  };
  return (
    <>
      <div className="table-toolbar incident-filter-toolbar">
        <NativeSelect aria-label="Saved incident view" value={selectedView} onChange={(e) => selectView(e.target.value)}>
          <NativeSelectOption value="">Saved views</NativeSelectOption>
          {(views.data ?? []).map((view) => <NativeSelectOption key={view.id} value={view.id}>{view.name}</NativeSelectOption>)}
        </NativeSelect>
        <Input aria-label="Saved view name" placeholder="Name this view" maxLength={60} value={viewName} onChange={(e) => setViewName(e.target.value)} />
        <Button variant="outline" size="sm" disabled={!viewName.trim() || saveView.isPending || deleteView.isPending} onClick={() => saveView.mutate({ id: selectedView, name: viewName.trim(), filters: currentFilters })}>{selectedView ? "Update view" : "Save view"}</Button>
        {selectedView ? <Button variant="ghost" size="sm" disabled={deleteView.isPending || saveView.isPending} onClick={() => deleteView.mutate(selectedView)}>Delete view</Button> : null}
        {selectedView ? <Button variant="ghost" size="sm" onClick={() => { setSelectedView(""); setViewName(""); }}>New view</Button> : null}
        {views.isError ? <span role="alert" className="status-error">Could not load saved views</span> : null}
        {viewError ? <span role="alert" className="status-error">{viewError}</span> : null}
        {viewNotice ? <span role="status" className="muted">{viewNotice}</span> : null}
      </div>
      <div className="table-toolbar incident-filter-toolbar">
        <div className="search-field"><Search size={15} /><Input aria-label="Search incidents" placeholder="Incident title or ID" maxLength={100} value={searchInput} onChange={(e) => { setSearchInput(e.target.value); reset(); }} /></div>
        <NativeSelect
          aria-label="Incident state"
          value={state}
          onChange={(e) => {
            setState(e.target.value);
            reset();
          }}
        >
          <NativeSelectOption value="">All states</NativeSelectOption>
          {incidentStates.map((s) => (
            <NativeSelectOption key={s} value={s}>
              {s}
            </NativeSelectOption>
          ))}
        </NativeSelect>
        <NativeSelect aria-label="Incident severity" value={severity} onChange={(e) => { setSeverity(e.target.value as IncidentListFilters["severity"]); reset(); }}>
          <NativeSelectOption value="">All severities</NativeSelectOption>
          {(["SEV-1", "SEV-2", "SEV-3", "SEV-4"] as const).map((value) => <NativeSelectOption key={value} value={value}>{value}</NativeSelectOption>)}
        </NativeSelect>
        <Input aria-label="Filter incidents by service" placeholder="Exact service" maxLength={100} value={serviceInput} onChange={(e) => { setServiceInput(e.target.value); reset(); }} />
        <Input aria-label="Filter incidents by owner" placeholder="Owner contains" maxLength={100} value={ownerInput} onChange={(e) => { setOwnerInput(e.target.value); reset(); }} />
        <NativeSelect aria-label="Incident detected time" value={range} onChange={(e) => {
          const next = e.target.value;
          setRange(next);
          setSince(rangeSince(next));
          reset();
        }}>
          <NativeSelectOption value="all">Any detected time</NativeSelectOption>
          <NativeSelectOption value="24h">Last 24 hours</NativeSelectOption>
          <NativeSelectOption value="7d">Last 7 days</NativeSelectOption>
          <NativeSelectOption value="30d">Last 30 days</NativeSelectOption>
        </NativeSelect>
        <NativeSelect aria-label="Sort incidents" value={sort} onChange={(e) => { setSort(e.target.value as IncidentListFilters["sort"]); reset(); }}>
          <NativeSelectOption value="updated_desc">Recently updated</NativeSelectOption>
          <NativeSelectOption value="detected_desc">Newest opened</NativeSelectOption>
          <NativeSelectOption value="detected_asc">Oldest opened</NativeSelectOption>
          <NativeSelectOption value="severity_asc">Highest severity</NativeSelectOption>
        </NativeSelect>
        <details className="incident-column-menu"><summary>Columns</summary><div>{registerColumns.slice(1).map((column) => <label key={column.id}><input type="checkbox" checked={columnVisibility[column.id!] !== false} onChange={(e) => setColumnVisibility((current) => ({ ...current, [column.id!]: e.target.checked }))} />{typeof column.header === "string" ? column.header : column.id}</label>)}</div></details>
        <Button variant="ghost" size="sm" onClick={() => { setState(""); setSeverity(""); setServiceInput(""); setOwnerInput(""); setSearchInput(""); setService(""); setOwner(""); setSearch(""); setRange("all"); setSince(""); setSort("updated_desc"); reset(); }}>Clear filters</Button>
        <Button
          variant="outline"
          onClick={() => {
            reset();
            void query.refetch();
          }}
        >
          Refresh
        </Button>
      </div>
      {query.isPending ? (
        <LoadingState />
      ) : query.isError ? (
        <div className="panel-padding">
          <ErrorState error={query.error} retry={() => query.refetch()} />
        </div>
      ) : query.data.items.length === 0 ? (
        <EmptyState
          title="No incidents in this view"
          description="Try another state, severity, service, owner, search term or detected-time range."
        />
      ) : <IncidentRegisterTable items={query.data.items} visibility={columnVisibility} onVisibilityChange={setColumnVisibility} />}
      <div className="pagination">
        <span>Page {cursors.length} · refresh to include newer updates</span>
        <div>
          <Button
            variant="outline"
            disabled={cursors.length === 1 || query.isFetching}
            onClick={() => setCursors((v) => v.slice(0, -1))}
          >
            Previous
          </Button>
          <Button
            variant="outline"
            disabled={
              !query.data?.more ||
              !query.data.next ||
              query.isFetching ||
              query.isError
            }
            onClick={() => setCursors((v) => [...v, query.data!.next!])}
          >
            Next
          </Button>
        </div>
      </div>
    </>
  );
}
function safeEvidenceURL(raw?: string) {
  if (!raw) return undefined;
  try {
    const u = new URL(raw);
    return ["http:", "https:"].includes(u.protocol) &&
      !u.username &&
      !u.password
      ? u.href
      : undefined;
  } catch {
    return undefined;
  }
}
export function IncidentDetail({ id, traceViewer = null }: { id: string; traceViewer?: string | null }) {
  const canEdit = usePermission("editIncident");
  const canEscalate = usePermission("escalateIncident");
  const [pages, setPages] = useState([0]);
  const after = pages[pages.length - 1];
  const query = useQuery({
    queryKey: ["incident", id, after],
    refetchInterval: false,
    refetchOnWindowFocus: false,
    queryFn: () => incidents.detail(id, after),
  });
  if (query.isPending) return <LoadingState />;
  if (query.isError)
    return <ErrorState error={query.error} retry={() => query.refetch()} />;
  const { incident: i, history, postmortem } = query.data;
  return (
    <>
      <PageHeader
        title={i.title}
        description={`${i.id} · ${i.environment} · ${i.service}`}
      >
        <Button variant="outline" asChild>
          <Link href="/incidents">Back to incidents</Link>
        </Button>
        {canEdit ? <IncidentEditor incident={i} /> : null}
        {canEscalate ? <IncidentEscalation incident={i} /> : null}
        <Button variant="outline" onClick={() => query.refetch()}>
          Refresh
        </Button>
      </PageHeader>
      <Panel
        title="Incident overview"
        description={`Detected ${timestamp(i.detected_at)} WIB · updated ${timestamp(i.updated_at)} WIB · revision ${i.version}`}
      >
        <div className="panel-padding incident-facts">
          <Severity value={i.severity} />
          <Badge variant="outline">{i.state}</Badge>
          <span>Owner: {i.owner || "Unassigned"}</span>
          <span>Owning team: {i.owning_team || "Unassigned"}</span>
          <span>Escalation: {i.escalation_level === 0 ? "None" : `Level ${i.escalation_level}${i.escalated_at ? ` · ${timestamp(i.escalated_at)} WIB` : ""}`}</span>
          <span>Affected users: {i.affected_users ?? "Unknown"}</span>
          <span>
            Affected transactions: {i.affected_transactions ?? "Unknown"}
          </span>
          <span>
            Error rate:{" "}
            {i.error_rate === null
              ? "Unknown"
              : `${(i.error_rate * 100).toFixed(2)}%`}
          </span>
          <span>
            Success rate:{" "}
            {i.success_rate === null
              ? "Unknown"
              : `${(i.success_rate * 100).toFixed(2)}%`}
          </span>
          <span>
            Latency: {i.latency_ms === null ? "Unknown" : `${i.latency_ms} ms`}
          </span>
        </div>
      </Panel>
      <IncidentTelemetry incident={i} />
      <IncidentMetrics id={i.id} />
      <IncidentImpact id={i.id} />
      {i.state === "Monitoring" ? <IncidentRecovery incident={i} /> : null}
      <IncidentLogs id={i.id} />
      <IncidentTraces id={i.id} viewer={traceViewer} />
      <IncidentInfrastructure id={i.id} />
      <IncidentPostmortem incident={i} report={postmortem} />
      <IncidentDeployments incident={i} />
      <div className="detail-grid incident-sections">
        <Panel title="Investigation">
          <dl className="panel-padding incident-notes">
            {[
              ["Impact", i.impact],
              ["Related deployment", i.related_deployment],
              ["Root cause", i.root_cause],
              ["Mitigation", i.mitigation],
              ["Resolution", i.resolution],
              ["Recovery validation", i.recovery_validation ? `${i.recovery_validation.observation} · observed ${timestamp(i.recovery_validation.observed_at)} · validated by ${i.recovery_validation.validated_by} at ${timestamp(i.recovery_validation.validated_at)}` : ""],
              ["Postmortem", i.postmortem_notes],
            ].filter(([label]) => label !== "Postmortem" || !postmortem).map(([label, value]) => (
              <div key={label}>
                <dt>{label}</dt>
                <dd>{value || "Not recorded"}</dd>
              </div>
            ))}
          </dl>
          {i.recovery_validation && safeEvidenceURL(i.recovery_validation.source_url) ? <div className="panel-padding"><a href={i.recovery_validation.source_url} target="_blank" rel="noopener noreferrer">Open recovery evidence ↗</a></div> : null}
        </Panel>
        <Panel title="Evidence and action items">
          <div className="panel-padding incident-notes">
            <h3>Recorded evidence</h3>
            {!i.evidence?.length ? (
              <p className="muted">No evidence attached.</p>
            ) : (
              i.evidence.map((e, index) => (
                <div key={index}>
                  <strong>{e.kind}</strong>
                  <p>{e.summary}</p>
                  {safeEvidenceURL(e.url) && (
                    <a
                      className="table-link"
                      href={safeEvidenceURL(e.url)}
                      target="_blank"
                      rel="noopener noreferrer"
                    >
                      Open evidence ↗
                    </a>
                  )}
                </div>
              ))
            )}
            <h3 id="action-items">Follow-up actions</h3>
            {!i.action_items?.length ? (
              <p className="muted">No action items recorded.</p>
            ) : (
              i.action_items.map((a, index) => (
                <div key={index}>
                  <Badge variant="outline">
                    {a.status}
                  </Badge>
                  <Badge variant="outline">{a.priority}</Badge>
                  <p>{a.title}</p>
                  <p className="muted">
                    {a.owner || "Unassigned"}
                    {a.due_at ? ` · due ${timestamp(a.due_at)} WIB` : ""}
                  </p>
                </div>
              ))
            )}
          </div>
        </Panel>
      </div>
      <Panel
        title="Audit timeline"
        description="Immutable revisions · oldest first · recorded actor identity"
      >
        <ol className="panel-padding incident-timeline">
          {history.map((h) => (
            <li key={h.version}>
              <div>
                <strong>
                  Revision {h.version} ·{" "}
                  {h.action === "escalated"
                    ? `Escalated to ${h.after.owning_team} · level ${h.after.escalation_level}`
                    : h.before?.state === h.after.state
                    ? "Details updated"
                    : h.after.state}
                </strong>
                <span className="muted">
                  {timestamp(h.at)} WIB · {h.actor}
                </span>
              </div>
              <p>{h.note}</p>
            </li>
          ))}
        </ol>
        <div className="pagination">
          <span>History page {pages.length}</span>
          <div>
            <Button
              variant="outline"
              disabled={pages.length === 1 || query.isFetching}
              onClick={() => setPages((p) => p.slice(0, -1))}
            >
              Earlier revisions
            </Button>
            <Button
              variant="outline"
              disabled={!query.data.history_more || query.isFetching}
              onClick={() => setPages((p) => [...p, query.data.history_next!])}
            >
              Later revisions
            </Button>
          </div>
        </div>
      </Panel>
    </>
  );
}
