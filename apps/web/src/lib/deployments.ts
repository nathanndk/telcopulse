import { z } from "zod";
import { request } from "./api";
import type { Environment } from "./contracts";

const deployment = z.object({
  id: z.string(),
  service: z.string(),
  version: z.string(),
  commit_sha: z.string(),
  environment: z.string(),
  deployer: z.string(),
  status: z.enum(["Pending", "Running", "Completed", "Failed", "Rolled Back"]),
  occurred_at: z.string().datetime({ offset: true }),
});
const detail = deployment.extend({
  events: z.array(z.object({
    event_id: z.string(),
    actor: z.string(),
    status: deployment.shape.status,
    occurred_at: z.string().datetime({ offset: true }),
  })),
});
export type Deployment = z.infer<typeof deployment>;
export const deployments = {
  list: (environment: Environment, service = "", since?: string, until?: string) =>
    request(`/deployments?${new URLSearchParams({ environment, service, limit: "100", ...(since ? { since } : {}), ...(until ? { until } : {}) })}`, z.array(deployment)),
  detail: (id: string) => request(`/deployments/${encodeURIComponent(id)}`, detail),
};
