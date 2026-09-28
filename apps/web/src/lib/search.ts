import { z } from "zod";
import { request } from "./api";
import type { Environment } from "./contracts";

const workspaceHit = z.object({
  source: z.enum(["incident", "transaction", "deployment", "simulation"]),
  resource_id: z.string(),
  title: z.string(),
  detail: z.string(),
  at: z.string().datetime({ offset: true }),
});
const workspaceResults = z.object({ items: z.array(workspaceHit).max(20) });

export type WorkspaceHit = z.infer<typeof workspaceHit>;
export function workspaceHitPath(hit: WorkspaceHit) {
  return `/${({ incident: "incidents", transaction: "transactions", deployment: "deployments", simulation: "simulations" })[hit.source]}/${encodeURIComponent(hit.resource_id)}`;
}
export const workspaceSearch = (environment: Environment, query: string) =>
  request(`/search?${new URLSearchParams({ environment, q: query })}`, workspaceResults);
