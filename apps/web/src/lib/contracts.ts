import { z } from "zod";
export const environmentSchema = z.enum(["development", "staging"]);
export type Environment = z.infer<typeof environmentSchema>;
export const customerSchema = z.object({
  id: z.string(),
  name: z.string(),
  msisdn_masked: z.string(),
  balance_idr: z.number(),
});
export const packageSchema = z.object({
  id: z.string(),
  name: z.string(),
  data_gb: z.number(),
  days: z.number(),
  price_idr: z.number(),
});
export const purchaseSchema = z.object({
  customer_id: z.string().min(1, "Select a customer"),
  package_id: z.string().min(1, "Select a package"),
  payment_method: z.enum([
    "Pulsa",
    "E-Wallet",
    "Credit Card",
    "Virtual Account",
  ]),
  environment: environmentSchema,
  replay_of: z.string().regex(/^TXN-[a-f0-9]{24}$/).optional(),
});
export const stepSchema = z.object({
  service: z.string(),
  operation: z.string(),
  status: z.enum(["SUCCESS", "FAILED", "PROCESSING"]),
  duration_ms: z.number(),
  span_id: z.string(),
});
export const transactionSchema = z.object({
  id: z.string(),
  replay_of: z.string().optional(),
  trace_id: z.string(),
  customer_id: z.string(),
  customer_name: z.string(),
  msisdn_masked: z.string(),
  package_id: z.string(),
  package_name: z.string(),
  payment_method: z.enum(["Pulsa", "E-Wallet", "Credit Card", "Virtual Account"]),
  environment: environmentSchema,
  status: z.enum(["SUCCESS", "FAILED", "PROCESSING"]),
  error_code: z.string(),
  amount_idr: z.number(),
  duration_ms: z.number(),
  created_at: z.string(),
  steps: z.array(stepSchema),
});
export const transactionPageSchema = z.object({
  items: z.array(transactionSchema),
  total: z.number(),
  page: z.number(),
  page_size: z.number(),
});
export const notificationStatusSchema = z.object({
  transaction_id: z.string(),
  status: z.enum(["PROCESSING", "AWAITING_DELIVERY", "DELIVERED"]),
  delivered_at: z.string().nullable(),
});
export const overviewSchema = z.object({
  total: z.number(),
  success: z.number(),
  failed: z.number(),
  success_rate: z.number(),
  p95_ms: z.number(),
  per_minute: z.number(),
  purchase_slo: z.object({
    window_start: z.string(),
    window_end: z.string(),
    target_percent: z.number(),
    total: z.number(),
    success: z.number(),
    failed: z.number(),
    current_percent: z.number().nullable(),
    allowed_failures: z.number(),
    budget_remaining: z.number(),
    budget_consumed_percent: z.number().nullable(),
  }),
  series: z.array(
    z.object({
      time: z.string(),
      total: z.number(),
      success_rate: z.number(),
      p95_ms: z.number(),
    }),
  ),
});
export type Customer = z.infer<typeof customerSchema>;
export type Package = z.infer<typeof packageSchema>;
export type Purchase = z.infer<typeof purchaseSchema>;
export type Transaction = z.infer<typeof transactionSchema>;
export type Overview = z.infer<typeof overviewSchema>;
