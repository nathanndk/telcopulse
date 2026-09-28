"use client";

import Link from "next/link";
import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { deployments, type Deployment } from "@/lib/deployments";
import { timestamp } from "@/lib/api";
import { useEnvironment } from "./providers";
import { Badge } from "./ui/badge";
import { Button } from "./ui/button";
import { Input } from "./ui/input";
import { NativeSelect, NativeSelectOption } from "./ui/native-select";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "./ui/table";
import { EmptyState, ErrorState, LoadingState, PageHeader, Panel } from "./common";
import type { Incident } from "@/lib/incidents";

function DeploymentStatus({ value }: { value: Deployment["status"] }) {
  return <Badge variant="outline" className={value === "Failed" || value === "Rolled Back" ? "status-error" : value === "Completed" ? "status-success" : "status-neutral"}>{value}</Badge>;
}

export function DeploymentList() {
  const { environment } = useEnvironment();
  const [service, setService] = useState("");
  const [selected, setSelected] = useState("");
  const query = useQuery({ queryKey: ["deployments", environment], queryFn: () => deployments.list(environment), refetchInterval: false });
  const items = query.data?.filter((item) => (!selected || item.status === selected) && item.service.toLowerCase().includes(service.trim().toLowerCase())) ?? [];
  return <>
    <PageHeader title="Deployments" description="Reported releases and rollback events in the selected environment, including labeled local simulations. Review timing against incidents; proximity alone is not causation.">
      <Button variant="outline" onClick={() => void query.refetch()}>Refresh</Button>
    </PageHeader>
    <Panel title="Recent deployment activity" description="Latest 24 hours · up to 100 records · reported events">
      <div className="table-toolbar">
        <Input aria-label="Filter deployments by service" placeholder="Service name, e.g. payment-service" value={service} onChange={(event) => setService(event.target.value)} />
        <NativeSelect aria-label="Deployment status" value={selected} onChange={(event) => setSelected(event.target.value)}>
          <NativeSelectOption value="">All statuses</NativeSelectOption>
          {["Pending", "Running", "Completed", "Failed", "Rolled Back"].map((value) => <NativeSelectOption key={value} value={value}>{value}</NativeSelectOption>)}
        </NativeSelect>
      </div>
      {query.isPending ? <LoadingState /> : query.isError ? <div className="panel-padding"><ErrorState error={query.error} retry={() => query.refetch()} /></div> : items.length === 0 ? <EmptyState title="No deployments in this view" description="No deployment events match this service, status and environment in the last 24 hours." /> :
        <Table><TableHeader><TableRow>{["Deployment", "Service", "Version", "Status", "Last event (WIB)", "Deployer"].map((heading) => <TableHead key={heading}>{heading}</TableHead>)}</TableRow></TableHeader><TableBody>{items.map((item) => <TableRow key={item.id}><TableCell><Link className="table-link mono" href={`/deployments/${encodeURIComponent(item.id)}`}>{item.id}</Link></TableCell><TableCell>{item.service}</TableCell><TableCell className="mono">{item.version}</TableCell><TableCell><DeploymentStatus value={item.status} /></TableCell><TableCell className="nowrap">{timestamp(item.occurred_at)}</TableCell><TableCell>{item.deployer}</TableCell></TableRow>)}</TableBody></Table>}
    </Panel>
  </>;
}

export function DeploymentDetail({ id }: { id: string }) {
  const query = useQuery({ queryKey: ["deployment", id], queryFn: () => deployments.detail(id), refetchOnWindowFocus: false });
  if (query.isPending) return <LoadingState />;
  if (query.isError) return <ErrorState error={query.error} retry={() => query.refetch()} />;
  const item = query.data;
  return <>
    <PageHeader title={`${item.service} ${item.version}`} description={`${item.id} · ${item.environment} · ${item.id.startsWith("sim-deploy-") ? "Local synthetic release marker" : "Reported deployment"}`}><Button variant="outline" asChild><Link href="/deployments">Back to deployments</Link></Button></PageHeader>
    <Panel title="Deployment record" description="Immutable reported event history; synthetic markers are behavioral simulations, and no record independently proves that runtime pods changed.">
      <dl className="panel-padding incident-notes">
        <div><dt>Status</dt><dd><DeploymentStatus value={item.status} /></dd></div>
        <div><dt>Commit</dt><dd className="mono">{item.commit_sha}</dd></div>
        <div><dt>Initial deployer</dt><dd>{item.deployer}</dd></div>
        <div><dt>Last event</dt><dd>{timestamp(item.occurred_at)} WIB</dd></div>
      </dl>
    </Panel>
    <Panel title="Deployment timeline" description="Reported transitions · oldest first">
      <ol className="panel-padding incident-timeline">{item.events.map((event) => <li key={event.event_id}><div><strong>{event.status}</strong><span className="muted">{timestamp(event.occurred_at)} WIB · {event.actor}</span></div><p className="mono muted">{event.event_id}</p></li>)}</ol>
    </Panel>
  </>;
}

export function IncidentDeployments({ incident }: { incident: Incident }) {
  const crossService = incident.title.startsWith("BusinessSuccessRateLow · ");
  const service = crossService ? "" : incident.service;
  const detected = new Date(incident.detected_at).getTime();
  const since = new Date(detected - 2 * 60 * 60 * 1000).toISOString();
  const until = new Date(detected + 30 * 60 * 1000).toISOString();
  const query = useQuery({ queryKey: ["incident-deployments", incident.id, service, incident.environment, since, until], queryFn: () => deployments.list(incident.environment as "development" | "staging", service, since, until), refetchOnWindowFocus: false });
  return <Panel title="Deployment evidence" description={`Reported events from 2 hours before to 30 minutes after detection · ${crossService ? "all services in this environment, because business success is a cross-service metric" : "same service and environment"}. Timing is a lead, not a root-cause conclusion.`}>
    {incident.related_deployment && <p className="panel-padding">Operator reference: <span className="mono">{incident.related_deployment}</span></p>}
    {query.isPending ? <LoadingState /> : query.isError ? <div className="panel-padding"><ErrorState error={query.error} retry={() => query.refetch()} /></div> : query.data.length === 0 ? <EmptyState title="No deployment events in this window" description="No event was reported for this environment near detection. This does not prove that no deployment occurred." /> :
      <Table><TableHeader><TableRow>{["Deployment", "Service", "Version", "Status", "Last event (WIB)"].map((heading) => <TableHead key={heading}>{heading}</TableHead>)}</TableRow></TableHeader><TableBody>{query.data.map((item) => <TableRow key={item.id}><TableCell><Link className="table-link mono" href={`/deployments/${encodeURIComponent(item.id)}`}>{item.id}</Link></TableCell><TableCell>{item.service}</TableCell><TableCell className="mono">{item.version}</TableCell><TableCell><DeploymentStatus value={item.status} /></TableCell><TableCell>{timestamp(item.occurred_at)}</TableCell></TableRow>)}</TableBody></Table>}
  </Panel>;
}
