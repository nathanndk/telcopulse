import { z } from "zod";
import { request } from "./api";
import type { LogWindow } from "./incident-logs";

const trace = z.object({
  trace_id: z.string().regex(/^[a-f0-9]{32}$/),
  started_at: z.string().datetime({ offset: true }),
  nodes: z.array(z.object({
    name: z.string(),
    kind: z.enum(["service", "database"]),
    max_duration_ms: z.number().nonnegative(),
    error: z.boolean(),
  })),
  edges: z.array(z.object({ from: z.string(), to: z.string() })),
});

const evidence = z.object({
  configured: z.boolean(),
  source: z.literal("jaeger"),
  window_start: z.string().datetime({ offset: true }),
  window_end: z.string().datetime({ offset: true }),
  items: z.array(trace),
});

export const incidentTraces = {
  list: (id: string, window: LogWindow) => request(
    `/incidents/${encodeURIComponent(id)}/traces?${new URLSearchParams({ window })}`,
    evidence,
  ),
};
