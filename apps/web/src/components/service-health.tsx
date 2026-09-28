"use client";
import { useQuery } from "@tanstack/react-query";
import { duration } from "@/lib/api";
import { readServiceHealth } from "@/lib/service-health";
import { ErrorState, LoadingState, StatusBadge } from "./common";
import {
  Table,
  TableHeader,
  TableHead,
  TableBody,
  TableRow,
  TableCell,
} from "./ui/table";

const names: Record<string, string> = {
  "auth-service": "Login",
  "api-gateway": "Purchase",
  "subscriber-service": "Subscriber",
  "package-service": "Package",
  "payment-service": "Payment",
  "notification-service": "Notification",
  "incident-service": "Incidents",
};
export function ServiceHealth() {
  const query = useQuery({
    queryKey: ["service-health"],
    queryFn: readServiceHealth,
    refetchInterval: 15000,
  });
  if (query.isPending) return <LoadingState />;
  if (query.isError)
    return <ErrorState error={query.error} retry={() => query.refetch()} />;
  return (
    <>
      <p className="panel-note">
        Shared runtime · Both transaction environments · HTTP measurements over
        the last 5 minutes. Business success is reported separately.
      </p>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Service</TableHead>
            <TableHead>Status</TableHead>
            <TableHead className="text-right">RPS</TableHead>
            <TableHead className="text-right">HTTP 5xx</TableHead>
            <TableHead className="text-right">P95</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {query.data.items.map((row) => (
            <TableRow key={row.id}>
              <TableCell>
                <div className="table-stack">
                  <strong>{names[row.id] ?? row.id}</strong>
                  <small>{row.id}</small>
                </div>
              </TableCell>
              <TableCell>
                <div className="table-stack">
                  <StatusBadge status={row.status} />
                  <small className="max-w-40 whitespace-normal">
                    {row.reason}
                  </small>
                </div>
              </TableCell>
              <TableCell className="text-right mono">
                {row.rps === null ? "—" : row.rps.toFixed(2)}
              </TableCell>
              <TableCell className="text-right mono">
                {row.error_rate === null
                  ? "—"
                  : `${(row.error_rate * 100).toFixed(2)}%`}
              </TableCell>
              <TableCell className="text-right mono">
                {row.p95_ms === null ? "—" : duration(row.p95_ms)}
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
      <p className="panel-note">
        Evaluated {new Date(query.data.observed_at).toLocaleTimeString()}. No
        traffic or incomplete telemetry remains Unknown. A failed scrape is
        Critical.
      </p>
    </>
  );
}
