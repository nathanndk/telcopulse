"use client";
import Link from "next/link";
import { useQuery } from "@tanstack/react-query";
import { simulations } from "@/lib/simulations";
import type { Environment } from "@/lib/contracts";
import { Panel, ErrorState, LoadingState, EmptyState } from "./common";
import { StartSimulation, StopSimulation } from "./simulation-controls";
import { usePermission } from "./providers";
import { Badge } from "./ui/badge";
import {
  Table,
  TableHeader,
  TableHead,
  TableBody,
  TableRow,
  TableCell,
} from "./ui/table";
export function SimulationRuns({ environment }: { environment: Environment }) {
  const canInject = usePermission("injectFailure");
  const query = useQuery({
    queryKey: ["simulations", environment],
    queryFn: () => simulations.list(environment),
    refetchInterval: 5000,
  });
  const active = query.data?.some((run) => run.active) ?? false;
  return (
    <Panel
      title="Controlled failure simulations"
      description={`${environment} · controlled faults · latest 20 runs`}
      action={
        <StartSimulation
          environment={environment}
          disabled={query.isPending || query.isError || active}
        />
      }
    >
      <p className="panel-note">
        Selection applies to the chosen environment. Kafka slowdown can delay
        other environments sharing the consumer. Runs expire automatically;
        start and stop reasons are audited as local-operator.
        {!canInject && " Your role can review runs but cannot start or stop them."}
      </p>
      {query.isPending ? (
        <LoadingState />
      ) : query.isError ? (
        <ErrorState error={query.error} retry={() => query.refetch()} />
      ) : !query.data.length ? (
        <EmptyState
          title="No failure simulations recorded"
          description="Start a bounded run to investigate real failures or latency, metrics and traces."
        />
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Run</TableHead>
              <TableHead>State</TableHead>
              <TableHead>Selection rate</TableHead>
              <TableHead>Expires</TableHead>
              <TableHead>Action</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {query.data.map((run) => (
              <TableRow key={run.id}>
                <TableCell>
                  <div className="table-stack">
                    <Link
                      className="table-link"
                      href={`/simulations/${run.id}`}
                    >
                      {run.id}
                    </Link>
                    <small className="max-w-64 whitespace-normal">
                      {run.scenario}
                      {run.delay_ms ? ` · ${run.delay_ms} ms` : ""} ·{" "}
                      {run.reason}
                    </small>
                  </div>
                </TableCell>
                <TableCell>
                  <Badge
                    variant="outline"
                    className={run.active ? "status-warning" : "status-neutral"}
                  >
                    {run.active
                      ? "Active"
                      : run.stopped_at
                        ? "Stopped"
                        : "Expired"}
                  </Badge>
                </TableCell>
                <TableCell className="mono">{run.percentage}%</TableCell>
                <TableCell>
                  <time dateTime={run.expires_at}>
                    {new Date(run.expires_at).toLocaleString()}
                  </time>
                </TableCell>
                <TableCell>
                  {run.active ? <StopSimulation run={run} /> : "—"}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
    </Panel>
  );
}
