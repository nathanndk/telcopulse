import { z } from "zod";
import { request } from "./api";
import type { Environment } from "./contracts";

const reportSummary = z.object({
  incident_id: z.string(),
  incident_title: z.string(),
  environment: z.string(),
  service: z.string(),
  severity: z.enum(["SEV-1", "SEV-2", "SEV-3", "SEV-4"]),
  incident_version: z.number().int().positive(),
  generated_at: z.string().datetime({ offset: true }),
  generated_by: z.string(),
  summary: z.string(),
  root_cause: z.string(),
  contributing_factors: z.string(),
  mitigation: z.string(),
  resolution: z.string(),
  what_went_well: z.string(),
  what_went_wrong: z.string(),
});

const reportPage = z.object({
  items: z.array(reportSummary),
  more: z.boolean(),
  next: z.string().optional(),
});

export type ReportSummary = z.infer<typeof reportSummary>;
export type ReportFilters = {
  environment: Environment;
  service: string;
  severity: "" | ReportSummary["severity"];
  search: string;
  cursor: string;
};

export const postmortems = {
  list: (filters: ReportFilters) =>
    request(
      `/incidents/postmortems?${new URLSearchParams({ ...filters, limit: "10" })}`,
      reportPage,
    ),
};
