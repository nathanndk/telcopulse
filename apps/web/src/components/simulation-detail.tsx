"use client";
import { useState } from "react";
import Link from "next/link";
import { useQuery } from "@tanstack/react-query";
import { simulations } from "@/lib/simulations";
import {
  PageHeader,
  Panel,
  ErrorState,
  LoadingState,
  EmptyState,
} from "./common";
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

export function SimulationDetail({ id }: { id: string }) {
  const [pages, setPages] = useState([""]);
  const cursor = pages[pages.length - 1];
  const query = useQuery({
    queryKey: ["simulation", id, cursor],
    queryFn: () => simulations.detail(id, cursor),
  });
  if (query.isPending) return <LoadingState />;
  if (query.isError)
    return <ErrorState error={query.error} retry={() => query.refetch()} />;
  const { run, audit, decisions, observed, selected, more, next, deployment_report } = query.data;
  return (
    <>
      <PageHeader
        title="Simulation investigation"
        description={`${run.id} · ${run.environment} · ${run.scenario}`}
      >
        <Button variant="outline" asChild>
          <Link href="/simulator">Back to simulator</Link>
        </Button>
        <Button
          variant="outline"
          onClick={() => {
            setPages([""]);
            void query.refetch();
          }}
        >
          Refresh run
        </Button>
      </PageHeader>
      <Panel
        title="Run overview"
        description="Local synthetic failure · operator decisions and recorded scope"
      >
        <div className="panel-padding incident-notes">
          <div className="incident-facts">
            <Badge
              variant="outline"
              className={run.active ? "status-warning" : "status-neutral"}
            >
              {run.active ? "Active" : run.stopped_at ? "Stopped" : "Expired"}
            </Badge>
            <span>Configured selection rate: {run.percentage}%</span>
            <span>Observed decisions: {observed}</span>
            <span>Selected for injection: {selected}</span>
          </div>
          <p>{run.reason}</p>
          {run.deployment_id && (
            <p>
              Synthetic payment release 1.4.0-sim-bad · deployment report: {deployment_report}.{" "}
              {deployment_report === "Completed" || deployment_report === "Rolled Back" ? (
                <Link className="table-link mono" href={`/deployments/${encodeURIComponent(run.deployment_id)}`}>
                  {run.deployment_id}
                </Link>
              ) : <span className="mono">{run.deployment_id}</span>}
              <span className="muted"> · reported marker, not an image rollout</span>
            </p>
          )}
          {run.delay_ms > 0 && (
            <p>
              {run.scenario === "kafka-consumer-lag"
                ? "Notification delay"
                : run.scenario === "database-timeout"
                  ? "Statement timeout"
                  : "Database delay"}
              : {run.delay_ms} ms{" "}
              {run.scenario === "kafka-consumer-lag"
                ? "per selected event while the run is active"
                : "per selected new reservation"}
            </p>
          )}
          <p className="muted">
            Started {new Date(run.started_at).toLocaleString()} · scheduled
            expiry {new Date(run.expires_at).toLocaleString()}
            {run.stopped_at
              ? ` · stopped ${new Date(run.stopped_at).toLocaleString()}`
              : ""}
          </p>
          <p className="muted">
            Selection counts describe simulation decisions, not confirmed
            affected users or completed failures. Inspect transaction outcomes
            before recording incident impact. Stop and expiry affect new
            decisions; existing retries retain their selection.
          </p>
        </div>
      </Panel>
      <Panel
        title="Simulation audit"
        description="Append-only start and stop records · natural expiry follows the recorded deadline"
      >
        <ol className="panel-padding incident-timeline">
          {audit.map((entry) => (
            <li key={entry.action}>
              <div>
                <strong>
                  {entry.action === "started"
                    ? "Simulation started"
                    : "Simulation stopped"}
                </strong>
                <span className="muted">
                  {new Date(entry.at).toLocaleString()} · {entry.actor}
                </span>
              </div>
              <p>{entry.note}</p>
            </li>
          ))}
        </ol>
      </Panel>
      <Panel
        title="Transaction decisions"
        description="Up to 20 per page · descending transaction ID · live pages may gain new decisions"
      >
        {!decisions.length ? (
          <EmptyState
            title="No decisions on this page"
            description="Transactions appear once payment requests a decision for this run."
          />
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Transaction</TableHead>
                <TableHead>Decision</TableHead>
                <TableHead>Observed</TableHead>
                <TableHead>Evidence</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {decisions.map((decision) => (
                <TableRow key={decision.transaction_id}>
                  <TableCell className="mono">
                    {decision.transaction_id}
                  </TableCell>
                  <TableCell>
                    {decision.inject ? "Fault selected" : "Not selected"}
                  </TableCell>
                  <TableCell>
                    <time dateTime={decision.at}>
                      {new Date(decision.at).toLocaleString()}
                    </time>
                  </TableCell>
                  <TableCell>
                    <Link
                      className="table-link"
                      href={`/transactions/${encodeURIComponent(decision.transaction_id)}`}
                    >
                      Investigate {decision.transaction_id}
                    </Link>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
        <div className="pagination">
          <span>Decision page {pages.length}</span>
          <div>
            <Button
              variant="outline"
              disabled={pages.length === 1 || query.isFetching}
              onClick={() => setPages((value) => value.slice(0, -1))}
            >
              Previous decisions
            </Button>
            <Button
              variant="outline"
              disabled={!more || !next || query.isFetching}
              onClick={() => setPages((value) => [...value, next!])}
            >
              Next decisions
            </Button>
          </div>
        </div>
      </Panel>
    </>
  );
}
