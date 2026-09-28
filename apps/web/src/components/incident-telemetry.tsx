"use client";
import { useQuery } from "@tanstack/react-query";
import { readServiceHealth } from "@/lib/service-health";
import type { Incident } from "@/lib/incidents";
import { duration } from "@/lib/api";
import {
  Panel,
  LoadingState,
  ErrorState,
  EmptyState,
  StatusBadge,
} from "./common";
import { Button } from "./ui/button";
import { IncidentEditor } from "./incident-editor";

export function IncidentTelemetry({ incident }: { incident: Incident }) {
  const query = useQuery({
    queryKey: ["service-health"],
    queryFn: readServiceHealth,
    refetchInterval: 15000,
  });
  const service = query.data?.items.find(
    (item) => item.id === incident.service,
  );
  const measured = (value: number | null | undefined, unit: string) =>
    value == null ? "Unknown" : `${value} ${unit}`;
  const summary =
    query.data && service
      ? `Prometheus HTTP snapshot; service=${service.id}; evaluated=${query.data.observed_at}; window=300 seconds; scope=shared-runtime (development and staging); status=${service.status}; scrape=${service.up == null ? "Unknown" : service.up === 1 ? "reachable" : "failed"}; RPS=${measured(service.rps, "requests/second")}; HTTP 5xx=${measured(service.error_rate, "fraction")}; P95=${measured(service.p95_ms, "ms")}. Current observation, not incident-time impact or proof of root cause.`
      : "";
  return (
    <Panel
      title="Live service telemetry"
      description="Current shared-runtime HTTP measurements · development and staging use the same processes"
    >
      {query.isPending ? (
        <LoadingState />
      ) : query.isError ? (
        <ErrorState error={query.error} retry={() => query.refetch()} />
      ) : !service || !query.data ? (
        <EmptyState
          title="No matching service telemetry"
          description="The affected service has no configured HTTP measurement source."
        />
      ) : (
        <div className="panel-padding incident-notes">
          <div className="incident-facts">
            <strong>{service.id}</strong>
            <StatusBadge status={service.status} />
            <span>RPS: {service.rps?.toFixed(2) ?? "Unknown"}</span>
            <span>
              HTTP 5xx:{" "}
              {service.error_rate === null
                ? "Unknown"
                : `${(service.error_rate * 100).toFixed(2)}%`}
            </span>
            <span>
              HTTP P95:{" "}
              {service.p95_ms === null ? "Unknown" : duration(service.p95_ms)}
            </span>
          </div>
          <p>{service.reason}</p>
          <p className="muted">
            Evaluated {query.data.observed_at} · trailing 5 minutes. These
            measurements describe current HTTP behavior, not business impact at
            detection time. Recovery does not resolve the incident
            automatically.
          </p>
          <div className="page-actions">
            <Button
              variant="outline"
              disabled={query.isFetching}
              onClick={() => query.refetch()}
            >
              Refresh telemetry
            </Button>
          </div>
        </div>
      )}
      <div className="panel-padding incident-notes">
        <IncidentEditor
          incident={incident}
          disabled={query.isError || query.isPending || !service}
          metricEvidence={{ kind: "metric", summary }}
        />
        <p className="muted">
          Review freezes this observation in the evidence draft. It is saved
          only after you submit an audit note. Up to 50 evidence entries.
        </p>
      </div>
    </Panel>
  );
}
