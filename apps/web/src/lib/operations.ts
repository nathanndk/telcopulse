import { z } from "zod";
import { request } from "./api";
import type { Environment } from "./contracts";

const window = {
  window_start: z.string().datetime({ offset: true }),
  window_end: z.string().datetime({ offset: true }),
};
const incidentOperations = z.object({
  ...window,
  incident_count: z.number().int().nonnegative(),
  sev1_count: z.number().int().nonnegative(),
  acknowledged_count: z.number().int().nonnegative(),
  mtta_seconds: z.number().nonnegative().nullable(),
  resolved_count: z.number().int().nonnegative(),
  mttr_seconds: z.number().nonnegative().nullable(),
  escalation_events: z.number().int().nonnegative(),
  repeat_count: z.number().int().nonnegative(),
});
const deploymentOperations = z.object({
  ...window,
  evaluated: z.number().int().nonnegative(),
  failed: z.number().int().nonnegative(),
  failure_rate_percent: z.number().min(0).max(100).nullable(),
});

export const operations = {
  incidents: (environment: Environment) =>
    request(`/incidents/operations?environment=${environment}`, incidentOperations),
  deployments: (environment: Environment) =>
    request(`/deployments/operations?environment=${environment}`, deploymentOperations),
};
