import { z } from "zod";
import { request } from "./api";
import type { LogWindow } from "./incident-logs";

const evidence = z.object({
  configured: z.boolean(),
  source: z.literal("datadog"),
  window_start: z.string().datetime({ offset: true }),
  window_end: z.string().datetime({ offset: true }),
  limited: z.boolean(),
  series: z.array(z.object({
    key: z.enum(["restarts", "memory", "cpu"]),
    label: z.string(),
    unit: z.enum(["count", "bytes", "nanocores"]),
    namespace: z.string(),
    pod: z.string(),
    points: z.array(z.object({ at: z.string().datetime({ offset: true }), value: z.number().nonnegative() })),
  })),
});

export const incidentInfrastructure = {
  list: (id: string, window: LogWindow) => request(
    `/incidents/${encodeURIComponent(id)}/infrastructure?${new URLSearchParams({ window })}`,
    evidence,
  ),
};
