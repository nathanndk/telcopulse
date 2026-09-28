import { z } from "zod";
import { request } from "./api";
import type { Environment } from "./contracts";

const auditEvent = z.object({
  id: z.string(),
  source: z.enum(["incident", "simulation", "deployment"]),
  environment: z.enum(["development", "staging"]),
  resource_id: z.string(),
  actor: z.string(),
  action: z.string(),
  note: z.string(),
  at: z.string().datetime({ offset: true }),
  changes: z.array(z.object({ field: z.string(), before: z.string().optional(), after: z.string().optional() })),
});

const auditPage = z.object({ items: z.array(auditEvent), more: z.boolean(), next: z.string().optional() });

export type AuditEvent = z.infer<typeof auditEvent>;
export const auditResourcePath = (event: AuditEvent) => ({
  incident: `/incidents/${encodeURIComponent(event.resource_id)}`,
  simulation: `/simulations/${encodeURIComponent(event.resource_id)}`,
  deployment: `/deployments/${encodeURIComponent(event.resource_id)}`,
})[event.source];
export type AuditSource = "" | AuditEvent["source"];
export type AuditFilters = {
  environment: Environment;
  source: AuditSource;
  actor: string;
  action: string;
  resource: string;
  cursor: string;
};

export const audit = {
  list: (filters: AuditFilters) => request(`/audit?${new URLSearchParams({ ...filters, limit: "25" })}`, auditPage),
};
