import { z } from "zod";
import { request } from "./api";
import type { Environment } from "./contracts";
import { actionPriorities, actionStatuses, incidentStates } from "./incidents";

const actionRow = z.object({
  incident_id: z.string(),
  incident_title: z.string(),
  incident_state: z.enum(incidentStates),
  incident_service: z.string(),
  incident_severity: z.enum(["SEV-1", "SEV-2", "SEV-3", "SEV-4"]),
  incident_updated_at: z.string().datetime({ offset: true }),
  position: z.number().int().positive(),
  title: z.string(),
  owner: z.string(),
  priority: z.enum(actionPriorities),
  status: z.enum(actionStatuses),
  due_at: z.string().datetime({ offset: true }).nullable(),
});

const actionPage = z.object({
  items: z.array(actionRow),
  more: z.boolean(),
  next: z.string().optional(),
});

export type ActionRow = z.infer<typeof actionRow>;
export type ActionStatusFilter = "active" | "all" | ActionRow["status"];
export type ActionFilters = {
  environment: Environment;
  status: ActionStatusFilter;
  priority: "" | ActionRow["priority"];
  owner: string;
  search: string;
  cursor: string;
};

export const actions = {
  list: (filters: ActionFilters) =>
    request(
      `/incidents/actions?${new URLSearchParams({ ...filters, limit: "25" })}`,
      actionPage,
    ),
};
