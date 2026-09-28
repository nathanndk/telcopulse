CREATE TABLE notification.dead_letter_replays (
 idempotency_key text PRIMARY KEY,
 topic text NOT NULL,
 partition_id integer NOT NULL,
 message_offset bigint NOT NULL,
 actor text NOT NULL,
 status text NOT NULL CHECK(status IN ('DELIVERED','ALREADY_DELIVERED','REJECTED')),
 reason text NOT NULL DEFAULT '',
 transaction_id text NOT NULL DEFAULT '',
 attempted_at timestamptz NOT NULL DEFAULT now(),
 FOREIGN KEY(topic,partition_id,message_offset)
  REFERENCES notification.dead_letters(topic,partition_id,message_offset)
);
CREATE INDEX notification_dead_letter_replays_source_idx
 ON notification.dead_letter_replays(topic,partition_id,message_offset,attempted_at DESC);
CREATE FUNCTION notification.prevent_replay_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 RAISE EXCEPTION 'dead-letter replay audit is append-only';
END;
$$;
CREATE TRIGGER immutable_dead_letter_replays BEFORE UPDATE OR DELETE
 ON notification.dead_letter_replays FOR EACH ROW EXECUTE FUNCTION notification.prevent_replay_mutation();
