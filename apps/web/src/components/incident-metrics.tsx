"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { CartesianGrid, Line, LineChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";
import { incidentMetrics, type IncidentMetricSeries } from "@/lib/incident-metrics";
import type { LogWindow } from "@/lib/incident-logs";
import { EmptyState, ErrorState, LoadingState, Panel } from "./common";
import { Button } from "./ui/button";
import { NativeSelect, NativeSelectOption } from "./ui/native-select";

function displayValue(series: IncidentMetricSeries, value: number) {
  if (series.unit === "fraction") return `${(value * 100).toFixed(1)}%`;
  if (series.unit === "ms") return `${value.toFixed(0)} ms`;
  return `${value.toFixed(2)} ${series.unit}`;
}

function timeLabel(value: number) {
  return new Date(value).toLocaleString("en-GB", { timeZone: "Asia/Jakarta", day: "2-digit", month: "short", hour: "2-digit", minute: "2-digit" });
}

function MetricChart({ series }: { series: IncidentMetricSeries }) {
  const points = series.points.map((point) => ({ at: Date.parse(point.at), value: series.unit === "fraction" ? point.value * 100 : point.value }));
  const last = series.points.at(-1);
  return <div className="incident-metric-card">
    <div className="incident-metric-heading"><div><h3>{series.label}</h3><p>{series.scope === "environment" ? "Incident environment · synthetic business outcomes" : "Affected service · shared development/staging process"}</p></div><strong>{last ? displayValue(series, last.value) : "No data"}</strong></div>
    {points.length ? <div className="incident-metric-chart" role="img" aria-label={`${series.label} over incident time, ${points.length} measurements`}>
      <ResponsiveContainer width="100%" height="100%"><LineChart data={points} margin={{ top: 8, right: 8, bottom: 0, left: 0 }}>
        <CartesianGrid vertical={false} stroke="var(--border)" strokeDasharray="3 3" />
        <XAxis dataKey="at" type="number" domain={["dataMin", "dataMax"]} tickFormatter={timeLabel} tick={{ fill: "var(--muted-foreground)", fontSize: 10 }} axisLine={false} tickLine={false} minTickGap={28} />
        <YAxis width={44} tick={{ fill: "var(--muted-foreground)", fontSize: 10 }} axisLine={false} tickLine={false} unit={series.unit === "fraction" ? "%" : undefined} />
        <Tooltip labelFormatter={(value) => timeLabel(Number(value))} formatter={(value) => displayValue(series, Number(value) / (series.unit === "fraction" ? 100 : 1))} contentStyle={{ background: "var(--card)", border: "1px solid var(--border)", borderRadius: 6 }} />
        <Line dataKey="value" name={series.label} stroke={series.key === "http_error" ? "var(--destructive)" : series.key === "business_success" ? "var(--success)" : "var(--info)"} strokeWidth={2} dot={false} isAnimationActive={false} connectNulls={false} />
      </LineChart></ResponsiveContainer>
    </div> : <p className="muted">No samples in this period. Check traffic, scrape health, retention and the selected window.</p>}
    {last ? <p className="muted">Latest sample {timeLabel(Date.parse(last.at))} WIB · {series.points.length} points</p> : null}
  </div>;
}

export function IncidentMetrics({ id }: { id: string }) {
  const [window, setWindow] = useState<LogWindow>("2h");
  const query = useQuery({ queryKey: ["incident-metrics", id, window], queryFn: () => incidentMetrics.list(id, window), refetchOnWindowFocus: false });
  return <Panel className="log-evidence-panel" title="Incident-period metrics" description="Prometheus measurements aligned to detection; absent samples are unknown, not zero or healthy." action={<div className="page-actions">
    <NativeSelect aria-label="Metric evidence window" value={window} onChange={(event) => setWindow(event.target.value as LogWindow)}>
      <NativeSelectOption value="2h">Detection +2 hours</NativeSelectOption>
      <NativeSelectOption value="24h">Detection +24 hours</NativeSelectOption>
      <NativeSelectOption value="72h">Detection +72 hours</NativeSelectOption>
    </NativeSelect>
    <Button size="sm" variant="outline" onClick={() => void query.refetch()} disabled={query.isFetching}>Refresh</Button>
  </div>}>
    {query.isPending ? <LoadingState /> : query.isError ? <div className="panel-padding"><ErrorState error={query.error} retry={() => void query.refetch()} /></div> : !query.data.configured ? <EmptyState title="Metric history is not configured" description="Connect the incident service to Prometheus to inspect the detection period." /> : query.data.series.every((series) => series.points.length === 0) ? <EmptyState title="No measured samples in this period" description="Check Prometheus retention, scrape health, traffic and the selected detection window." /> : <div className="panel-padding">
      <p className="muted incident-metric-note">Prometheus · {query.data.window_start} to {query.data.window_end} · {query.data.step_seconds}s samples. Business series are environment-scoped; HTTP series reflect the shared service process.</p>
      <div className="incident-metric-grid">{query.data.series.map((series) => <MetricChart key={series.key} series={series} />)}</div>
    </div>}
  </Panel>;
}
