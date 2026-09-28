import { z } from "zod";
import { request } from "./api";

const item = z.object({
  source_topic: z.string(),
  source_partition: z.number().int().nonnegative(),
  source_offset: z.number().int().nonnegative(),
  reason: z.enum(["invalid purchase event", "event identity conflict"]),
  created_at: z.string().datetime({ offset: true }),
  payload_bytes: z.number().int().nonnegative(),
  payload_sha256: z.string().regex(/^[a-f0-9]{64}$/),
  publication: z.enum(["UNTRACKED", "PENDING", "PUBLISHED"]),
  replay_status: z.enum(["DELIVERED", "ALREADY_DELIVERED", "REJECTED"]).optional(),
  replay_actor: z.string().optional(),
  replayed_at: z.string().datetime({ offset: true }).optional(),
});
const page = z.object({ items: z.array(item), more: z.boolean(), next: z.string().optional() });
const replayResult = z.object({
  source_topic: z.string(),
  source_partition: z.number().int().nonnegative(),
  source_offset: z.number().int().nonnegative(),
  status: z.enum(["DELIVERED", "ALREADY_DELIVERED", "REJECTED"]),
  reason: z.string(),
  transaction_id: z.string(),
  actor: z.string(),
  attempted_at: z.string().datetime({ offset: true }),
});
const replayHistoryPage = z.object({ items: z.array(replayResult), more: z.boolean(), next: z.string().optional() });

export type DeadLetterReason = "" | z.infer<typeof item>["reason"];
export type DeadLetterRecord = z.infer<typeof item>;
export const deadLetters = {
  list: (reason: DeadLetterReason, cursor: string) => request(
    `/notifications/dead-letters?${new URLSearchParams({ reason, cursor, limit: "25" })}`,
    page,
  ),
  replay: (partition: number, offset: number, key: string) => request(
    `/notifications/dead-letters/${partition}/${offset}/replay`,
    replayResult,
    { method: "POST", headers: { "Idempotency-Key": key } },
  ),
  history: (partition: number, offset: number, cursor: string) => request(
    `/notifications/dead-letters/${partition}/${offset}/replays?${new URLSearchParams({ cursor, limit: "10" })}`,
    replayHistoryPage,
  ),
};
