import { z } from "zod";
import { request } from "./api";
import type { LogWindow } from "./incident-logs";

const metricEvidence = z.object({
  configured: z.boolean(),
  source: z.literal("prometheus"),
  window_start: z.string().datetime({ offset: true }),
  window_end: z.string().datetime({ offset: true }),
  step_seconds: z.number().int().nonnegative(),
  series: z.array(z.object({
    key: z.enum(["business_success", "business_tpm", "http_rps", "http_error", "http_p95"]),
    label: z.string(),
    unit: z.string(),
    scope: z.enum(["environment", "shared-runtime"]),
    points: z.array(z.object({ at: z.string().datetime({ offset: true }), value: z.number().nonnegative() })),
  })),
});

export type IncidentMetricSeries = z.infer<typeof metricEvidence>["series"][number];

export const incidentMetrics = {
  list: (id: string, window: LogWindow) => request(
    `/incidents/${encodeURIComponent(id)}/metrics?${new URLSearchParams({ window })}`,
    metricEvidence,
  ),
};
