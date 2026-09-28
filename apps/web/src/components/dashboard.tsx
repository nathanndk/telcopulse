"use client";
import Link from "next/link";
import { ActiveIncidents, useIncidentOverview } from "./active-incidents";
import { useQuery } from "@tanstack/react-query";
import {
  Activity,
  ArrowUpRight,
  Clock3,
  FlaskConical,
  RefreshCw,
  ShieldCheck,
  ArrowLeftRight,
  AlertTriangle,
  Radio,
} from "lucide-react";
import {
  ResponsiveContainer,
  LineChart,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  BarChart,
  Bar,
} from "recharts";
import { api, duration } from "@/lib/api";
import type { Overview } from "@/lib/contracts";
import { useEnvironment } from "./providers";
import {
  PageHeader,
  Panel,
  ErrorState,
  LoadingState,
  EmptyState,
} from "./common";
import { Button } from "./ui/button";
import { Badge } from "./ui/badge";
import { TransactionTable } from "./transaction-table";
import { ServiceHealth } from "./service-health";
import { DeadLetterRegister } from "./dead-letter-register";
import { PurchaseSLO } from "./purchase-slo";
import { OperationalMetrics } from "./operational-metrics";
function Trend({
  overview,
  latency = false,
}: {
  overview: Overview;
  latency?: boolean;
}) {
  if (!overview.series.length)
    return (
      <div className="chart-empty">
        <Activity size={26} />
        <strong>Waiting for transaction data</strong>
        <span>Run a purchase to begin measuring.</span>
      </div>
    );
  const data = overview.series.map((p) => ({
    ...p,
    label: new Date(p.time).toLocaleTimeString("en-GB", {
      hour: "2-digit",
      minute: "2-digit",
      timeZone: "Asia/Jakarta",
    }),
  }));
  return (
    <div
      className="chart"
      aria-label={
        latency
          ? "Observed P95 latency by minute"
          : "Observed business success rate by minute"
      }
    >
      <ResponsiveContainer width="100%" height="100%">
        {latency ? (
          <BarChart data={data}>
            <CartesianGrid
              vertical={false}
              stroke="var(--border)"
              strokeDasharray="3 3"
            />
            <XAxis
              dataKey="label"
              tick={{ fill: "var(--muted-foreground)", fontSize: 10 }}
              axisLine={false}
              tickLine={false}
            />
            <YAxis
              tick={{ fill: "var(--muted-foreground)", fontSize: 10 }}
              axisLine={false}
              tickLine={false}
              width={42}
            />
            <Tooltip
              contentStyle={{
                background: "var(--card)",
                border: "1px solid var(--border)",
                borderRadius: 6,
              }}
            />
            <Bar
              name="P95 (ms)"
              dataKey="p95_ms"
              fill="var(--info)"
              maxBarSize={18}
              radius={[3, 3, 0, 0]}
            />
          </BarChart>
        ) : (
          <LineChart data={data}>
            <CartesianGrid
              vertical={false}
              stroke="var(--border)"
              strokeDasharray="3 3"
            />
            <XAxis
              dataKey="label"
              tick={{ fill: "var(--muted-foreground)", fontSize: 10 }}
              axisLine={false}
              tickLine={false}
            />
            <YAxis
              domain={[0, 100]}
              tick={{ fill: "var(--muted-foreground)", fontSize: 10 }}
              axisLine={false}
              tickLine={false}
              width={42}
              unit="%"
            />
            <Tooltip
              contentStyle={{
                background: "var(--card)",
                border: "1px solid var(--border)",
                borderRadius: 6,
              }}
            />
            <Line
              name="Success rate (%)"
              type="monotone"
              dataKey="success_rate"
              stroke="var(--success)"
              strokeWidth={2}
              dot={{ r: 3 }}
              isAnimationActive={false}
            />
          </LineChart>
        )}
      </ResponsiveContainer>
    </div>
  );
}
export function Dashboard() {
  const { environment } = useEnvironment();
  const incidentQuery = useIncidentOverview(environment);
  const query = useQuery({
    queryKey: ["overview", environment],
    queryFn: () => api.overview(environment),
  });
  return (
    <>
      <PageHeader
        title="ITOC Overview"
        description="Service health, business performance and the signals that matter."
      >
        <Button
          variant="outline"
          size="sm"
          onClick={() => {
            void query.refetch();
            void incidentQuery.refetch();
          }}
          disabled={query.isFetching}
        >
          <RefreshCw data-icon="inline-start" />
          Refresh
        </Button>
        <Button size="sm" asChild>
          <Link href="/simulator">
            <FlaskConical data-icon="inline-start" />
            Run transaction
          </Link>
        </Button>
      </PageHeader>
      {query.isPending ? (
        <LoadingState />
      ) : query.isError ? (
        <>
          <ErrorState error={query.error} retry={() => query.refetch()} />
          <ActiveIncidents environment={environment} />
        </>
      ) : (
        <>
          <div className="status-bar">
            <span
              className={query.data.failed ? "warning-text" : "success-text"}
            >
              <Radio size={16} />
              {!query.data.total
                ? "Awaiting business telemetry"
                : query.data.failed
                  ? "Business failures observed"
                  : "Observed purchases successful"}
            </span>
            <span>
              {environment} <span className="divider-dot">·</span> Last 60
              minutes <span className="divider-dot">·</span> Refreshes every 15s
            </span>
          </div>
          <div className="metric-strip">
            {[
              {
                label: "Overall success rate",
                value: query.data.total
                  ? `${query.data.success_rate.toFixed(2)}%`
                  : "—",
                note: `${query.data.success} successful / ${query.data.total} total`,
                icon: ShieldCheck,
                color: "success-text",
              },
              {
                label: "Transactions / min",
                value: query.data.per_minute.toLocaleString(),
                note: "Completed in the last 60 seconds",
                icon: ArrowLeftRight,
                color: "info-text",
              },
              {
                label: "Active incidents",
                value:
                  incidentQuery.isError || !incidentQuery.data
                    ? "—"
                    : String(incidentQuery.data.active),
                note: incidentQuery.isError
                  ? "Incident data unavailable"
                  : incidentQuery.data
                    ? `${incidentQuery.data.critical} SEV-1 / SEV-2 · unresolved`
                    : "Loading incident data",
                icon: AlertTriangle,
                color: incidentQuery.data?.critical ? "error-text" : "muted",
              },
              {
                label: "P95 latency",
                value: query.data.total ? duration(query.data.p95_ms) : "—",
                note: "Measured purchase processing time",
                icon: Clock3,
                color: "info-text",
              },
            ].map((m) => (
              <div className="metric" key={m.label}>
                <div className="metric-label">
                  {m.label}
                  <m.icon size={16} className={m.color} />
                </div>
                <strong>{m.value}</strong>
                <small>{m.note}</small>
              </div>
            ))}
          </div>
          <ActiveIncidents environment={environment} />
          <PurchaseSLO slo={query.data.purchase_slo} />
          <OperationalMetrics environment={environment} />
          <div className="dashboard-grid">
            <Panel
              title="Service health"
              action={
                <Button variant="link" size="sm" asChild>
                  <Link href="/services">
                    View services
                    <ArrowUpRight />
                  </Link>
                </Button>
              }
              className="health-panel"
            >
              <ServiceHealth />
            </Panel>
            <Panel
              title="Business success rate"
              description="Purchase outcomes · 1-minute buckets"
              className="trend-panel"
            >
              <Trend overview={query.data} />
              <div className="chart-caption">
                <i className="legend-dot success" /> Purchase success{" "}
                <span>30-day objective {query.data.purchase_slo.target_percent.toFixed(1)}%</span>
              </div>
            </Panel>
            <Panel
              title="Latency (P95)"
              description="Observed processing time · milliseconds"
              className="trend-panel"
            >
              <Trend overview={query.data} latency />
              <div className="chart-caption">
                <i className="legend-dot info" /> Purchase duration{" "}
                <span>Last hour</span>
              </div>
            </Panel>
          </div>
          <Panel
            title="Recent transactions"
            description="Follow each purchase from subscriber validation to package activation."
            action={
              <Button variant="link" size="sm" asChild>
                <Link href="/transactions">
                  View all
                  <ArrowUpRight />
                </Link>
              </Button>
            }
          >
            <TransactionTable compact key={environment} />
          </Panel>
          <div className="foundation-note">
            <Activity size={16} />
            <span>Service environment</span>
            <p>
              Prometheus alerts create incidents for operator investigation.
              Vendor integrations and production access controls remain in
              progress.
            </p>
            <Badge variant="outline">Synthetic traffic</Badge>
          </div>
        </>
      )}
    </>
  );
}
export function Services() {
  return (
    <>
      <PageHeader
        title="Services"
        description="Observe the customer journey and identify gaps in telemetry."
      />
      <Panel
        title="Digital service inventory"
        description="Live Prometheus measurements from the shared local runtime."
      >
        <ServiceHealth />
      </Panel>
      <DeadLetterRegister />
      <Panel
        title="Current runtime boundary"
        description="Phase 2 · Independent Go services"
      >
        <EmptyState
          title="One coherent purchase flow"
          description="The gateway calls subscriber, package and payment services over authenticated internal HTTP; notifications consume durable Kafka events. Each service commits its own state; interrupted purchases resume from durable workflow records."
        />
      </Panel>
    </>
  );
}
