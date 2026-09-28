-- Retain the originating span for links from independent recovery attempts.
ALTER TABLE purchase_workflows ADD COLUMN IF NOT EXISTS traceparent text NOT NULL DEFAULT '';
