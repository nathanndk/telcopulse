import { z } from "zod";
import { request } from "./api";

const logEvidence = z.object({
  configured: z.boolean(),
  source: z.literal("splunk"),
  window_start: z.string().datetime({ offset: true }),
  window_end: z.string().datetime({ offset: true }),
  items: z.array(z.object({
    at: z.string(),
    level: z.enum(["INFO", "WARN", "ERROR"]),
    service: z.string(),
    message: z.string(),
    trace_id: z.string(),
    transaction_id: z.string(),
    error_code: z.string(),
  })),
});

export type LogWindow = "2h" | "24h" | "72h";

export const incidentLogs = {
  list: (id: string, window: LogWindow) => request(
    `/incidents/${encodeURIComponent(id)}/logs?${new URLSearchParams({ window })}`,
    logEvidence,
  ),
};
