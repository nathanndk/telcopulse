CREATE TABLE event_outbox (
 event_id text PRIMARY KEY,
 topic text NOT NULL,
 partition_key text NOT NULL,
 payload jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 published_at timestamptz,
 attempts integer NOT NULL DEFAULT 0,
 available_at timestamptz NOT NULL DEFAULT now(),
 last_error text NOT NULL DEFAULT ''
);
CREATE INDEX event_outbox_pending ON event_outbox(available_at,created_at) WHERE published_at IS NULL;
CREATE TABLE notification.inbox (
 event_id text PRIMARY KEY,
 payload jsonb NOT NULL,
 processed_at timestamptz NOT NULL DEFAULT now()
);
-- Poison messages are retained durably before their Kafka offset is committed.
CREATE TABLE notification.dead_letters (
 topic text NOT NULL,
 partition_id integer NOT NULL,
 message_offset bigint NOT NULL,
 payload bytea NOT NULL,
 reason text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(topic,partition_id,message_offset)
);
