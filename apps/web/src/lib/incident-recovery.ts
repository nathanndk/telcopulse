import { z } from "zod";
import { request } from "./api";

const assessment = z.object({
  applicable: z.boolean(),
  scope: z.literal("purchase_path_environment"),
  status: z.enum(["not_applicable", "not_monitoring", "collecting", "insufficient_traffic", "still_failing", "meets_target"]),
  evaluated_at: z.string().datetime({ offset: true }),
  monitoring_at: z.string().datetime({ offset: true }).optional(),
  earlier_start: z.string().datetime({ offset: true }),
  split_at: z.string().datetime({ offset: true }),
  window_end: z.string().datetime({ offset: true }),
  earlier_total: z.number().int().nonnegative(),
  earlier_success: z.number().int().nonnegative(),
  earlier_failed: z.number().int().nonnegative(),
  recent_total: z.number().int().nonnegative(),
  recent_success: z.number().int().nonnegative(),
  recent_failed: z.number().int().nonnegative(),
  minimum_outcomes: z.number().int().positive(),
  target_success_rate: z.number().min(0).max(1),
});

export type RecoveryAssessment = z.infer<typeof assessment>;
export const incidentRecovery = {
  assess: (id: string) => request(`/incidents/${encodeURIComponent(id)}/recovery-assessment`, assessment),
};
