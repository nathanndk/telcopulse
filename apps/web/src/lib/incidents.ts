import { z } from "zod";
import { request } from "./api";
import type { Environment } from "./contracts";
export const incidentStates = [
  "Detected",
  "Acknowledged",
  "Investigating",
  "Identified",
  "Mitigating",
  "Monitoring",
  "Resolved",
  "Postmortem",
] as const;
export const actionPriorities = ["P1", "P2", "P3"] as const;
export const actionStatuses = ["Open", "In Progress", "Blocked", "Completed"] as const;
const summary = z.object({
  id: z.string(),
  title: z.string(),
  severity: z.enum(["SEV-1", "SEV-2", "SEV-3", "SEV-4"]),
  state: z.enum(incidentStates),
  owner: z.string(),
  owning_team: z.string().default(""),
  escalation_level: z.number().int().nonnegative().default(0),
  service: z.string(),
  environment: z.string(),
  version: z.number().int(),
  created_at: z.string().datetime({ offset: true }),
  detected_at: z.string().datetime({ offset: true }),
  updated_at: z.string().datetime({ offset: true }),
  affected_users: z.number().nullable(),
  affected_transactions: z.number().nullable(),
});
const record = summary.extend({
  escalated_at: z.string().datetime({ offset: true }).nullable().default(null),
  impact: z.string(),
  root_cause: z.string(),
  mitigation: z.string(),
  resolution: z.string(),
  recovery_validation: z.object({
    observation: z.string(),
    source_url: z.string(),
    observed_at: z.string().datetime({ offset: true }),
    validated_by: z.string(),
    validated_at: z.string().datetime({ offset: true }),
  }).nullable().optional(),
  postmortem_notes: z.string(),
  related_deployment: z.string(),
  error_rate: z.number().nullable(),
  success_rate: z.number().nullable(),
  latency_ms: z.number().nullable(),
  evidence: z
    .array(
      z.object({
        kind: z.string(),
        summary: z.string(),
        url: z.string().optional(),
      }),
    )
    .nullable(),
  action_items: z
    .array(
      z.object({
        title: z.string(),
        owner: z.string(),
        done: z.boolean(),
        priority: z.enum(actionPriorities).optional(),
        status: z.enum(actionStatuses).optional(),
        due_at: z.string().datetime({ offset: true }).optional(),
      }).transform((item) => ({
        ...item,
        priority: item.priority ?? "P2" as const,
        status: item.status ?? (item.done ? "Completed" : "Open") as typeof actionStatuses[number],
      })),
    )
    .nullable(),
});
const postmortemReport = z.object({
  incident_id: z.string(),
  incident_version: z.number().int(),
  generated_at: z.string().datetime({ offset: true }),
  generated_by: z.string(),
  summary: z.string(),
  impact: z.string(),
  detection: z.string(),
  timeline: z.array(z.object({
    version: z.number().int(),
    action: z.string(),
    state: z.enum(incidentStates),
    actor: z.string(),
    note: z.string(),
    at: z.string().datetime({ offset: true }),
  })),
  root_cause: z.string(),
  contributing_factors: z.string(),
  mitigation: z.string().optional(),
  resolution: z.string(),
  recovery_validation: z.object({
    observation: z.string(), source_url: z.string(),
    observed_at: z.string().datetime({ offset: true }),
    validated_by: z.string(), validated_at: z.string().datetime({ offset: true }),
  }).optional(),
  what_went_well: z.string(),
  what_went_wrong: z.string(),
  action_items: z.array(z.object({
    title: z.string(),
    owner: z.string(),
    priority: z.enum(actionPriorities),
    status: z.enum(actionStatuses),
    done: z.boolean(),
    due_at: z.string().datetime({ offset: true }).optional(),
  })).nullable(),
});
const detail = z.object({
  incident: record,
  postmortem: postmortemReport.optional(),
  history: z.array(
    z.object({
      version: z.number(),
      action: z.string().optional(),
      actor: z.string(),
      note: z.string(),
      at: z.string().datetime({ offset: true }),
      before: record.nullable(),
      after: record,
    }),
  ),
  history_more: z.boolean(),
  history_next: z.number().optional(),
});
export const editableIncident = record.omit({
  id: true,
  environment: true,
  service: true,
  owning_team: true,
  escalation_level: true,
  escalated_at: true,
  state: true,
  version: true,
  created_at: true,
  detected_at: true,
  updated_at: true,
  recovery_validation: true,
});
export type Incident = z.infer<typeof record>;
export type PostmortemReport = z.infer<typeof postmortemReport>;
export type IncidentCreation = {
  title: string;
  severity: Incident["severity"];
  owner: string;
  impact: string;
  service: string;
  environment: Environment;
};
export type IncidentListFilters = {
  environment: Environment;
  state: string;
  severity: "" | "SEV-1" | "SEV-2" | "SEV-3" | "SEV-4";
  service: string;
  owner: string;
  search: string;
  since: string;
  sort: "updated_desc" | "detected_desc" | "detected_asc" | "severity_asc";
  cursor: string;
};
export const incidentColumns = ["incident", "severity", "state", "service", "owner", "owning_team", "detected_at", "updated_at", "actions"] as const;
export const savedViewFilters = z.object({
  state: z.string(),
  severity: z.string(),
  service: z.string(),
  owner: z.string(),
  search: z.string(),
  range: z.enum(["all", "24h", "7d", "30d"]),
  sort: z.enum(["updated_desc", "detected_desc", "detected_asc", "severity_asc"]).default("updated_desc"),
  columns: z.array(z.enum(incidentColumns)).default([...incidentColumns]),
});
export type SavedViewFilters = z.infer<typeof savedViewFilters>;
const savedView = z.object({
  id: z.string(),
  environment: z.enum(["development", "staging"]),
  name: z.string(),
  filters: savedViewFilters,
  created_at: z.string().datetime({ offset: true }),
  updated_at: z.string().datetime({ offset: true }),
});
export type SavedView = z.infer<typeof savedView>;
export const savedViews = {
  list: (environment: Environment) => request(`/incidents/saved-views?environment=${environment}`, z.array(savedView)),
  create: (environment: Environment, name: string, filters: SavedViewFilters) => request("/incidents/saved-views", savedView, {
    method: "POST", body: JSON.stringify({ environment, name, filters }),
  }),
  update: (id: string, environment: Environment, name: string, filters: SavedViewFilters) => request(`/incidents/saved-views/${encodeURIComponent(id)}`, savedView, {
    method: "PUT", body: JSON.stringify({ environment, name, filters }),
  }),
  delete: (id: string) => request(`/incidents/saved-views/${encodeURIComponent(id)}`, z.object({ deleted: z.boolean() }), {
    method: "DELETE",
  }),
};
export const incidents = {
  overview: (environment: Environment) =>
    request(
      `/incidents/overview?${new URLSearchParams({ environment })}`,
      z.object({
        active: z.number().int().nonnegative(),
        critical: z.number().int().nonnegative(),
        items: z.array(summary),
      }),
    ),
  create: (input: IncidentCreation, key: string) =>
    request("/incidents", record, {
      method: "POST",
      headers: { "Idempotency-Key": key },
      body: JSON.stringify(input),
    }),
  update: (
    current: Incident,
    changes: Partial<z.infer<typeof editableIncident>> & {
      state: Incident["state"];
      note: string;
      recovery_validation?: { observation: string; source_url: string; observed_at: string };
    },
  ) =>
    request(`/incidents/${encodeURIComponent(current.id)}`, record, {
      method: "PUT",
      body: JSON.stringify({
        ...editableIncident.parse(current),
        ...changes,
        expected_version: current.version,
      }),
    }),
  escalate: (
    current: Incident,
    input: { team: string; owner: string; severity: Incident["severity"] | ""; reason: string },
  ) =>
    request(`/incidents/${encodeURIComponent(current.id)}/escalations`, record, {
      method: "POST",
      body: JSON.stringify({ ...input, expected_version: current.version }),
    }),
  generatePostmortem: (
    current: Incident,
    input: { summary: string; detection: string; contributing_factors: string; what_went_well: string; what_went_wrong: string; note: string },
  ) =>
    request(`/incidents/${encodeURIComponent(current.id)}/postmortem`, postmortemReport, {
      method: "POST",
      body: JSON.stringify({ ...input, expected_version: current.version }),
    }),
  list: (filters: IncidentListFilters) =>
    request(
      `/incidents?${new URLSearchParams({ ...filters, limit: "20" })}`,
      z.object({
        items: z.array(summary),
        more: z.boolean(),
        next: z.string().optional(),
      }),
    ),
  detail: (id: string, after = 0) =>
    request(
      `/incidents/${encodeURIComponent(id)}?history_after=${after}`,
      detail,
    ),
};
export type IncidentSummary = z.infer<typeof summary>;
