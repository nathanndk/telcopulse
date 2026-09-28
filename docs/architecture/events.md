# Event delivery

The gateway commits `telcopulse.purchase.completed.v1` with the completed purchase, workflow and audit record. The envelope has a stable `event_id`, schema version, occurrence time and operation snapshot containing transaction/trace/environment identifiers and business outcome. It excludes customer names and raw MSISDNs. Both successful and failed business outcomes generate events.

`event_outbox` is the durable source for publication. Replicas select due records with `FOR UPDATE SKIP LOCKED`; acknowledgment marks publication. Failed attempts retain the payload, error and attempt count, with exponential backoff capped at 60 seconds. Kafka downtime does not hold customer requests open.

The producer uses all-replica acknowledgment and bounded request timeouts. Kafka producer idempotency is disabled intentionally: its sequence-preservation rules can postpone failure of ambiguous in-flight writes indefinitely. Outbox publication is explicitly **at least once**; database consumer deduplication is the correctness boundary. A crash after broker acknowledgment but before updating the outbox can publish a duplicate.

Notification consumption uses consumer group `telcopulse-notifications-v1`, manual offset commits, one record at a time and rebalance blocking during processing. The inbox and synthetic delivery receipt commit atomically before the Kafka offset. A crash between those commits causes safe redelivery. Reusing an event or transaction ID with different contents is rejected. This does not promise exactly-once delivery to an external SMS/email provider; no such provider is connected.

Invalid or conflicting messages are durably quarantined in `notification.dead_letters`, keyed by topic, partition and offset, before acknowledgment. The same database transaction adds a redacted event to `notification.event_outbox`; its relay publishes to `telcopulse.notification.dead-letter.v1`. The topic contains the source topic, partition, offset, fixed failure reason, payload byte count and SHA-256 digest, never the raw payload. Outbox retries preserve the event during broker outages; duplicate source processing retains one logical dead-letter event. Storage outages leave the source record unacknowledged for retry. Raw quarantined payloads remain database-only evidence and are never logged. The operator replay command reads the retained bytes under a row lock, revalidates them, and commits any synthetic delivery and an append-only replay audit in one transaction. A rejected revalidation is audited without changing the source or delivery. An idempotency key serializes uncertain retries; the source lock serializes distinct keys for the same record. The Services register shows the latest outcome and a source-scoped, paginated history of all attempts; the history projection omits raw bytes and command keys.

Compose runs one Apache Kafka 4.1.1 broker/controller for local development with persistent storage, three default partitions and 24-hour retention. This is not a highly available production cluster. The plaintext listener on localhost:19092 supports native development; container services use kafka:9092. Authentication/TLS, topic provisioning/retention policy, lag metrics and production replication belong to later infrastructure/security work.

## Evidence

Go integration tests cover rollback, duplicate delivery, identity conflicts, quarantine, broker error retention, replay and real broker delivery. `tests/integration/kafka_recovery.py` checks stopped-consumer and paused-broker recovery against Compose; `tests/integration/kafka_dead_letter.py` verifies an actual redacted dead-letter record on the broker. The Services console presents a paginated, authenticated register of safe metadata, publication state and latest replay attempt. Engineer or Administrator can confirm reprocessing. Raw payloads remain database-only; fixing malformed original bytes is outside the replay command.

## Service-owned facts

The payment service commits `telcopulse.payment.state.v1` to `payment.event_outbox` with every new RESERVED, FAILED, CONFIRMED or RELEASED state. Replaying an operation emits no additional logical event. RESERVED/FAILED use sequence 1; CONFIRMED/RELEASED use sequence 2. Consumers compare sequence within a producer and transaction rather than assuming delivery order across partitions, relay replicas or topics.

The notification service commits `telcopulse.notification.delivered.v1` to `notification.event_outbox` atomically with its inbox and synthetic delivery receipt. Its causation ID references the purchase-completed event. Duplicate consumption does not emit another delivery event.

Each service runs its own publisher; payment and notification publishing do not depend on a running gateway relay. All three outboxes retain failed attempts until broker recovery. Fact payloads include no raw MSISDN or customer name. A later telemetry/incident consumer will use these facts; no such consumer is claimed yet.
