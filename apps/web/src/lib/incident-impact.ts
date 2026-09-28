import { z } from "zod";
import { request } from "./api";
import type { LogWindow } from "./incident-logs";

const failedPurchase = z.object({
  id: z.string(),
  trace_id: z.string(),
  customer_id: z.string(),
  error_code: z.string(),
  created_at: z.string().datetime({ offset: true }),
});
const purchaseImpact = z.object({
  applicable: z.boolean(),
  scope: z.literal("purchase_path_environment"),
  window_start: z.string().datetime({ offset: true }),
  window_end: z.string().datetime({ offset: true }),
  total: z.number().int().nonnegative(),
  success: z.number().int().nonnegative(),
  failed: z.number().int().nonnegative(),
  failed_customers: z.number().int().nonnegative(),
  items: z.array(failedPurchase),
  more: z.boolean(),
  next: z.string().optional(),
});
export const incidentImpact = {
  list: (id: string, window: LogWindow, cursor = "") => request(`/incidents/${encodeURIComponent(id)}/purchase-impact?${new URLSearchParams({ window, cursor })}`, purchaseImpact),
};
