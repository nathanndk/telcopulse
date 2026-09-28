"use client";
import Link from "next/link";
import { useQuery } from "@tanstack/react-query";
import {
  ExternalLink,
  ArrowLeft,
  ArrowRight,
  Copy,
  CheckCircle2,
  AlertCircle,
  Database,
} from "lucide-react";
import { traceLinks } from "@/lib/trace-links";
import { toast } from "sonner";
import { api, duration, money, timestamp } from "@/lib/api";
import {
  PageHeader,
  Panel,
  ErrorState,
  LoadingState,
  StatusBadge,
} from "./common";
import { Button } from "./ui/button";
import { ReplayFailedTransaction } from "./replay-failed-transaction";
import { TransactionNotification } from "./transaction-notification";
import {
  Table,
  TableHeader,
  TableHead,
  TableBody,
  TableRow,
  TableCell,
} from "./ui/table";
export function TransactionDetail({ id, traceViewer }: { id: string; traceViewer: string | null }) {
  const query = useQuery({
    queryKey: ["transaction", id],
    queryFn: () => api.transaction(id),
  });
  if (query.isPending) return <LoadingState />;
  if (query.isError)
    return <ErrorState error={query.error} retry={() => query.refetch()} />;
  const t = query.data;
  const traces = traceLinks(traceViewer, t.trace_id, t.id);
  async function copy() {
    try {
      await navigator.clipboard.writeText(t.trace_id);
      toast.success("Trace ID copied");
    } catch {
      toast.error("Could not copy. Select the trace ID below.");
    }
  }
  return (
    <>
      <Link className="back-link" href="/transactions">
        <ArrowLeft size={14} /> Back to transactions
      </Link>
      <PageHeader
        title={
          t.status === "SUCCESS"
            ? "Purchase completed"
            : t.status === "PROCESSING"
              ? "Purchase processing"
              : "Purchase investigation"
        }
        description={t.id}
      >
        <StatusBadge status={t.status} />
        <ReplayFailedTransaction transaction={t} />
        <Button variant="outline" size="sm" onClick={copy}>
          <Copy />
          Copy trace ID
        </Button>
      </PageHeader>
      <div className="detail-metadata">
        <div>
          <small>Package</small>
          <strong>{t.package_name}</strong>
        </div>
        <div>
          <small>Amount</small>
          <strong>{money(t.amount_idr)}</strong>
        </div>
        <div>
          <small>Environment</small>
          <strong>{t.environment}</strong>
        </div>
        <div>
          <small>Duration</small>
          <strong>{duration(t.duration_ms)}</strong>
        </div>
        <div>
          <small>Created (WIB)</small>
          <strong>{timestamp(t.created_at)}</strong>
        </div>
      </div>
      <div className="detail-grid">
        <Panel title="Transaction overview">
          <dl className="details-list">
            <dt>Customer</dt>
            <dd>{t.customer_name}</dd>
            <dt>Masked MSISDN</dt>
            <dd className="mono">{t.msisdn_masked}</dd>
            <dt>Payment method</dt>
            <dd>{t.payment_method}</dd>
            <dt>Business outcome</dt>
            <dd>
              <StatusBadge status={t.status} />
            </dd>
            <dt>Error code</dt>
            <dd>{t.error_code || "None"}</dd>
            <dt>Trace ID</dt>
            <dd className="mono break-all">{t.trace_id}</dd>
            {t.replay_of && <>
              <dt>Replay source</dt>
              <dd><Link href={`/transactions/${encodeURIComponent(t.replay_of)}`} className="text-link mono">{t.replay_of}</Link></dd>
            </>}
          </dl>
        </Panel>
        <Panel
          title="Purchase stage evidence"
          description="Service calls and durable notification enqueue. Queuing confirms storage; Kafka delivery happens asynchronously."
        >
          <ol className="trace-steps">
            {t.steps.map((step) => (
              <li key={step.span_id}>
                <span
                  className={
                    step.status === "SUCCESS" ? "success-text" : "error-text"
                  }
                >
                  {step.status === "SUCCESS" ? (
                    <CheckCircle2 size={18} />
                  ) : (
                    <AlertCircle size={18} />
                  )}
                </span>
                <div>
                  <strong>{step.operation}</strong>
                  <small>{step.service}</small>
                </div>
                <span className="mono">{duration(step.duration_ms)}</span>
              </li>
            ))}
          </ol>
        </Panel>
      </div>
      <TransactionNotification id={t.id} />
      <Panel
        title="Distributed traces"
        description="Inspect HTTP, database and Kafka spans in Jaeger. Workflow attempts include recovery traces linked to the original request."
      >
        <div className="p-5">
        {traces ? (
          <div className="flex flex-wrap gap-3">
            {traces.original && <Button variant="outline" size="sm" asChild>
              <a href={traces.original} target="_blank" rel="noopener noreferrer">
                <ExternalLink aria-hidden="true" /> Open original trace
              </a>
            </Button>}
            <Button variant="outline" size="sm" asChild>
              <a href={traces.attempts} target="_blank" rel="noopener noreferrer">
                <ExternalLink aria-hidden="true" /> Find workflow attempts
              </a>
            </Button>
          </div>
        ) : <p>Trace viewer is not configured for this deployment.</p>}
        <p className="mt-3 text-sm text-muted-foreground">
          Opens in a new tab. New traces can take a few seconds to appear. Local traces expire on viewer restart; older or unexported transactions may have no results. Attempt search covers the last two days.
        </p>
        </div>
      </Panel>
      <Panel
        title="Dependency flow"
        description="The gateway coordinates idempotent service operations and persists the final result."
      >
        <div className="trace-flow">
          {t.steps.map((step, i) => (
            <div className="flow-pair" key={step.span_id}>
              <div className="flow-node">
                <StatusBadge status={step.status} />
                <strong>{step.service}</strong>
                <small>{duration(step.duration_ms)}</small>
              </div>
              {i < t.steps.length - 1 && <ArrowRight size={18} />}
            </div>
          ))}
          <ArrowRight size={18} />
          <div className="flow-node">
            <Database size={19} />
            <strong>PostgreSQL</strong>
            <small>Transaction persisted</small>
          </div>
        </div>
      </Panel>
      <Panel
        title="Correlated evidence"
        description="Persisted workflow stages. Stage references identify these records; exported span IDs are available in the trace viewer."
      >
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Service</TableHead>
              <TableHead>Outcome</TableHead>
              <TableHead>Operation</TableHead>
              <TableHead>Stage reference</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {t.steps.map((s) => (
              <TableRow key={s.span_id}>
                <TableCell>{s.service}</TableCell>
                <TableCell>
                  <StatusBadge status={s.status} />
                </TableCell>
                <TableCell>{s.operation}</TableCell>
                <TableCell className="mono">{s.span_id}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </Panel>
    </>
  );
}
