import { z } from "zod";
import { request } from "./api";
const measurement = z.number().finite().nonnegative().nullable();
export const serviceHealthSchema = z.object({
  scope: z.literal("shared-runtime"),
  observed_at: z.string().datetime({ offset: true }),
  window_seconds: z.literal(300),
  items: z.array(
    z.object({
      id: z.string(),
      status: z.enum(["Healthy", "Degraded", "Critical", "Unknown"]),
      reason: z.string(),
      up: measurement,
      rps: measurement,
      error_rate: measurement,
      p95_ms: measurement,
    }),
  ),
});

export const readServiceHealth = () =>
  request("/services/health", serviceHealthSchema);
