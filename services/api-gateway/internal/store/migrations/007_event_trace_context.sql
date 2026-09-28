-- Transport metadata is separate from immutable domain payloads/deduplication.
ALTER TABLE public.event_outbox ADD COLUMN IF NOT EXISTS traceparent text NOT NULL DEFAULT '';
ALTER TABLE payment.event_outbox ADD COLUMN IF NOT EXISTS traceparent text NOT NULL DEFAULT '';
ALTER TABLE notification.event_outbox ADD COLUMN IF NOT EXISTS traceparent text NOT NULL DEFAULT '';
