"use client";

import Link from "next/link";
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Bell } from "lucide-react";
import { audit, auditResourcePath } from "@/lib/audit";
import { timestamp } from "@/lib/api";
import { incidents } from "@/lib/incidents";
import type { Environment } from "@/lib/contracts";
import { Button } from "./ui/button";
import { Badge } from "./ui/badge";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "./ui/dialog";

export function OperationsInbox({ environment }: { environment: Environment }) {
  const [open, setOpen] = useState(false);
  const active = useQuery({
    queryKey: ["incidents", "overview", environment],
    queryFn: () => incidents.overview(environment),
    refetchInterval: 60_000,
    refetchOnWindowFocus: false,
  });
  const recent = useQuery({
    queryKey: ["operations-inbox", environment],
    queryFn: () => audit.list({ environment, source: "", actor: "", action: "", resource: "", cursor: "" }),
    enabled: open,
    refetchOnWindowFocus: false,
  });
  const count = active.data?.active;
  const refresh = () => {
    void active.refetch();
    void recent.refetch();
  };
  return <>
    <Button variant="ghost" size="icon-sm" className="inbox-trigger"
      aria-label={active.isError ? "Operations inbox, incident count unavailable" : count === undefined ? "Operations inbox" : `Operations inbox, ${count} active ${count === 1 ? "incident" : "incidents"}`}
      title="Operations inbox" onClick={() => { setOpen(true); void active.refetch(); }}>
      <Bell aria-hidden="true" size={17} />
      {count !== undefined && count > 0 && <span className="inbox-count" aria-hidden="true">{count > 99 ? "99+" : count}</span>}
    </Button>
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogContent className="operations-inbox">
        <DialogHeader>
          <DialogTitle>Operations inbox</DialogTitle>
          <DialogDescription>Current incidents and recorded changes in {environment}. Open a record to continue investigation.</DialogDescription>
        </DialogHeader>
        <div className="inbox-toolbar"><span>Live sources · refresh for the latest record</span><Button variant="outline" size="sm" onClick={refresh}>Refresh</Button></div>
        <section className="inbox-section" aria-labelledby="inbox-active-heading">
          <div className="inbox-section-heading"><h3 id="inbox-active-heading">Active incidents</h3>{count !== undefined && <Badge variant="outline" className={active.data!.critical > 0 ? "status-error" : "status-neutral"}>{count} active</Badge>}</div>
          {active.isPending ? <p role="status">Loading incidents…</p> : active.isError ? <div role="alert" className="inbox-error"><p>Incident source unavailable. Current count is unknown.</p><Button variant="outline" size="sm" onClick={() => void active.refetch()}>Retry incidents</Button></div> : count === 0 ? <p>No active incidents recorded in this environment. Check service telemetry before concluding health.</p> : <ul className="inbox-list">{active.data.items.map((item) => <li key={item.id}>
            <Link href={`/incidents/${encodeURIComponent(item.id)}`} onClick={() => setOpen(false)}><strong>{item.title}</strong><span>{item.id} · {item.service} · {item.state}</span></Link>
            <Badge variant="outline" className={item.severity === "SEV-1" || item.severity === "SEV-2" ? "status-error" : "status-neutral"}>{item.severity}</Badge>
          </li>)}</ul>}
          {active.data && count !== undefined && count > active.data.items.length && <p className="inbox-more">Showing {active.data.items.length} of {count}. <Link href="/incidents" onClick={() => setOpen(false)}>Open incident register</Link></p>}
        </section>
        <section className="inbox-section" aria-labelledby="inbox-recent-heading">
          <div className="inbox-section-heading"><h3 id="inbox-recent-heading">Recent operations</h3><Link href="/audit" onClick={() => setOpen(false)}>Full audit log</Link></div>
          {recent.isPending ? <p role="status">Loading recorded changes…</p> : recent.isError ? <div role="alert" className="inbox-error"><p>Audit source unavailable. Recent changes are unknown.</p><Button variant="outline" size="sm" onClick={() => void recent.refetch()}>Retry audit</Button></div> : recent.data.items.length === 0 ? <p>No operations recorded in this environment yet.</p> : <ul className="inbox-list">{recent.data.items.slice(0, 5).map((event) => <li key={event.id}>
            <Link href={auditResourcePath(event)} onClick={() => setOpen(false)}><strong>{event.source} · {event.action}</strong><span>{event.resource_id} · {timestamp(event.at)} WIB</span></Link>
          </li>)}</ul>}
        </section>
      </DialogContent>
    </Dialog>
  </>;
}
