"use client";
import Link from "next/link";
import { useQuery } from "@tanstack/react-query";
import { incidents } from "@/lib/incidents";
import { timestamp } from "@/lib/api";
import type { Environment } from "@/lib/contracts";
import { Panel, ErrorState, EmptyState, LoadingState } from "./common";
import { Button } from "./ui/button";
import { Badge } from "./ui/badge";
import {
  Table,
  TableHeader,
  TableHead,
  TableBody,
  TableRow,
  TableCell,
} from "./ui/table";
export function useIncidentOverview(environment: Environment) {
  return useQuery({
    queryKey: ["incidents", "overview", environment],
    queryFn: () => incidents.overview(environment),
  });
}
export function ActiveIncidents({ environment }: { environment: Environment }) {
  const query = useIncidentOverview(environment);
  return (
    <Panel
      title="Active incidents"
      description="Detected through Monitoring · highest severity first · source alert recovery does not close an incident"
      action={
        <Button variant="link" asChild>
          <Link href="/incidents">Open incident register</Link>
        </Button>
      }
    >
      {query.isPending ? (
        <LoadingState />
      ) : query.isError ? (
        <div className="panel-padding">
          <ErrorState error={query.error} retry={() => query.refetch()} />
        </div>
      ) : query.data.active === 0 ? (
        <EmptyState
          title="No active incidents recorded"
          description="No unresolved incidents in this environment. This does not establish that every service is healthy."
        />
      ) : (
        <>
          <p className="panel-note">
            {query.data.active} active · {query.data.critical} SEV-1 / SEV-2 ·
            showing {query.data.items.length}
          </p>
          <Table>
            <TableHeader>
              <TableRow>
                {[
                  "Incident",
                  "Severity",
                  "State",
                  "Owner",
                  "Updated (WIB)",
                ].map((s) => (
                  <TableHead key={s}>{s}</TableHead>
                ))}
              </TableRow>
            </TableHeader>
            <TableBody>
              {query.data.items.map((i) => (
                <TableRow key={i.id}>
                  <TableCell>
                    <Link className="table-link" href={`/incidents/${i.id}`}>
                      {i.title}
                    </Link>
                    <div className="muted">{i.service}</div>
                  </TableCell>
                  <TableCell>
                    <Badge
                      variant="outline"
                      className={
                        i.severity === "SEV-1" || i.severity === "SEV-2"
                          ? "status-error"
                          : "status-neutral"
                      }
                    >
                      {i.severity}
                    </Badge>
                  </TableCell>
                  <TableCell>{i.state}</TableCell>
                  <TableCell>{i.owner || "Unassigned"}</TableCell>
                  <TableCell className="nowrap">
                    {timestamp(i.updated_at)}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </>
      )}
    </Panel>
  );
}
