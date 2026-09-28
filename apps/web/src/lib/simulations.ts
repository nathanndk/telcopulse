import { z } from "zod";
import { request } from "./api";
import type { Environment } from "./contracts";
const run = z.object({
  id: z.string(),
  environment: z.enum(["development", "staging"]),
  scenario: z.enum([
    "payment-decline",
    "database-latency",
    "database-timeout",
    "kafka-consumer-lag",
    "bad-deployment",
  ]),
  delay_ms: z.number().int().nonnegative(),
  deployment_id: z.string(),
  percentage: z.number().int().min(1).max(100),
  started_at: z.string().datetime({ offset: true }),
  expires_at: z.string().datetime({ offset: true }),
  stopped_at: z.string().datetime({ offset: true }).nullable(),
  reason: z.string(),
  active: z.boolean(),
});
export const simulationInput = z
  .object({
    scenario: z.enum([
      "payment-decline",
      "database-latency",
      "database-timeout",
      "kafka-consumer-lag",
      "bad-deployment",
    ]),
    delay_ms: z.number().int().min(0).max(4500),
    percentage: z.number().int().min(1).max(100),
    duration_seconds: z.number().int().min(30).max(900),
    reason: z
      .string()
      .trim()
      .min(1, "Explain why this failure is needed")
      .max(1000),
  })
  .refine((v) => v.scenario === "payment-decline" || v.scenario === "bad-deployment" || v.delay_ms >= 100, {
    path: ["delay_ms"],
    message: "Use 100–4500 ms",
  });
export type SimulationInput = z.infer<typeof simulationInput>;
export type SimulationRun = z.infer<typeof run>;
export type SimulationCommand = SimulationInput & {
  environment: Environment;
};
export const simulations = {
  detail: (id: string, cursor = "") =>
    request(
      `/simulations/${encodeURIComponent(id)}?${new URLSearchParams({ cursor })}`,
      z.object({
        run,
        deployment_report: z.string(),
        audit: z.array(
          z.object({
            action: z.enum(["started", "stopped"]),
            actor: z.string(),
            note: z.string(),
            at: z.string().datetime({ offset: true }),
          }),
        ),
        observed: z.number().int().nonnegative(),
        selected: z.number().int().nonnegative(),
        decisions: z.array(
          z.object({
            transaction_id: z.string(),
            inject: z.boolean(),
            at: z.string().datetime({ offset: true }),
          }),
        ),
        more: z.boolean(),
        next: z.string().optional(),
      }),
    ),
  list: (environment: Environment) =>
    request(`/simulations?environment=${environment}`, z.array(run)),
  start: (input: SimulationCommand, key: string) =>
    request("/simulations", run, {
      method: "POST",
      headers: { "Idempotency-Key": key },
      body: JSON.stringify(input),
    }),
  stop: (id: string, reason: string) =>
    request(`/simulations/${encodeURIComponent(id)}/stop`, run, {
      method: "POST",
      body: JSON.stringify({ reason }),
    }),
};
