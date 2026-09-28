package notification

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/twmb/franz-go/pkg/kgo"
	"go.opentelemetry.io/otel/codes"
	"telcopulse/services/shared/events"
)

type invalidEvent struct{ reason string }

func (e *invalidEvent) Error() string { return e.reason }

// Deliver atomically deduplicates the event and records a synthetic notification.
// A duplicate ID with different contents is rejected rather than silently accepted.
func Deliver(ctx context.Context, pool *pgxpool.Pool, payload []byte) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if e := tx.Rollback(cleanup); e != nil && !errors.Is(e, pgx.ErrTxClosed) {
			slog.Error("release notification transaction", "error", e)
		}
	}()
	_, _, err = deliverInto(ctx, tx, payload)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// deliverInto lets operator reprocessing commit delivery and audit in one transaction.
func deliverInto(ctx context.Context, tx pgx.Tx, payload []byte) (string, bool, error) {
	var e events.PurchaseCompleted
	d := json.NewDecoder(bytes.NewReader(payload))
	d.DisallowUnknownFields()
	if len(payload) > 16384 || d.Decode(&e) != nil || d.Decode(&struct{}{}) != io.EOF || e.Validate() != nil {
		return "", false, &invalidEvent{"invalid purchase event"}
	}
	_, err := tx.Exec(ctx, `INSERT INTO notification.inbox(event_id,payload) VALUES($1,$2) ON CONFLICT DO NOTHING`, e.ID, payload)
	if err != nil {
		return "", false, err
	}
	var same bool
	if err = tx.QueryRow(ctx, `SELECT payload=$2::jsonb FROM notification.inbox WHERE event_id=$1`, e.ID, payload).Scan(&same); err != nil {
		return "", false, err
	}
	if !same {
		return "", false, &invalidEvent{"event identity conflict"}
	}
	op, err := json.Marshal(e.Operation)
	if err != nil {
		return "", false, err
	}
	delivery, err := tx.Exec(ctx, `INSERT INTO notification.deliveries(transaction_id,request) VALUES($1,$2) ON CONFLICT DO NOTHING`, e.Operation.TransactionID, op)
	if err != nil {
		return "", false, err
	}
	if err = tx.QueryRow(ctx, `SELECT request=$2::jsonb FROM notification.deliveries WHERE transaction_id=$1`, e.Operation.TransactionID, op).Scan(&same); err != nil {
		return "", false, err
	}
	if !same {
		return "", false, &invalidEvent{"notification identity conflict"}
	}
	if delivery.RowsAffected() == 1 {
		if err = events.EnqueueDelivery(ctx, tx, e); err != nil {
			return "", false, err
		}
	}
	return e.Operation.TransactionID, delivery.RowsAffected() == 1, nil
}

// Process retains poison records before allowing offset acknowledgment. Transient
// storage failures return an error, leaving the record unacknowledged for retry.
func Process(ctx context.Context, pool *pgxpool.Pool, r *kgo.Record) error {
	return processWithDecision(ctx, pool, r, nil, slog.Default())
}

func processWithDecision(ctx context.Context, pool *pgxpool.Pool, r *kgo.Record, decide DecisionFunc, log *slog.Logger) (processErr error) {
	ctx, span := events.ConsumerSpan(ctx, r)
	defer func() {
		if processErr != nil {
			span.SetStatus(codes.Error, "notification processing failed")
		}
		span.End()
	}()
	if err := delayRecord(ctx, r, decide, log); err != nil {
		return err
	}
	err := Deliver(ctx, pool, r.Value)
	var invalid *invalidEvent
	if !errors.As(err, &invalid) {
		return err
	}
	span.SetStatus(codes.Error, "invalid event quarantined")
	return quarantine(ctx, pool, r, invalid.reason)
}

type deadLetterEvent struct {
	ID              string    `json:"event_id"`
	Version         int       `json:"schema_version"`
	Type            string    `json:"type"`
	OccurredAt      time.Time `json:"occurred_at"`
	SourceTopic     string    `json:"source_topic"`
	SourcePartition int32     `json:"source_partition"`
	SourceOffset    int64     `json:"source_offset"`
	Reason          string    `json:"reason"`
	PayloadBytes    int       `json:"payload_bytes"`
	PayloadSHA256   string    `json:"payload_sha256"`
}

// quarantine commits the raw restricted record and redacted outbox fact together.
// A retry at the same source offset cannot enqueue a second logical fact.
func quarantine(ctx context.Context, pool *pgxpool.Pool, r *kgo.Record, reason string) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if e := tx.Rollback(cleanup); e != nil && !errors.Is(e, pgx.ErrTxClosed) {
			slog.Error("release dead-letter transaction", "error", e)
		}
	}()
	_, err = tx.Exec(ctx, `INSERT INTO notification.dead_letters(topic,partition_id,message_offset,payload,reason) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, r.Topic, r.Partition, r.Offset, r.Value, reason)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(r.Value)
	event := deadLetterEvent{
		ID:              fmt.Sprintf("notification.dead-letter:%s:%d:%d", r.Topic, r.Partition, r.Offset),
		Version:         1,
		Type:            events.DeadLetterTopic,
		OccurredAt:      time.Now().UTC(),
		SourceTopic:     r.Topic,
		SourcePartition: r.Partition,
		SourceOffset:    r.Offset,
		Reason:          reason,
		PayloadBytes:    len(r.Value),
		PayloadSHA256:   fmt.Sprintf("%x", digest),
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO notification.event_outbox(event_id,topic,partition_key,payload,traceparent) VALUES($1,$2,$3,$4,$5) ON CONFLICT(event_id) DO NOTHING`, event.ID, event.Type, fmt.Sprintf("%s:%d", r.Topic, r.Partition), payload, events.TraceParent(ctx))
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Consume processes one record at a time with manual commits and bounded retries.
// Rebalances wait until database work and the synchronous offset commit finish.
func Consume(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger, client *kgo.Client) {
	ConsumeWithDecision(ctx, pool, log, client, nil)
}

// ConsumeWithDecision applies optional local simulation delays before delivery.
func ConsumeWithDecision(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger, client *kgo.Client, decide DecisionFunc) {
	defer client.AllowRebalance()
	for ctx.Err() == nil {
		fetches := client.PollRecords(ctx, 1)
		if ctx.Err() != nil {
			return
		}
		if errs := fetches.Errors(); len(errs) > 0 {
			log.Warn("Kafka notification fetch retry", "error", errs[0].Err)
		}
		for _, record := range fetches.Records() {
			for ctx.Err() == nil {
				attempt, cancel := context.WithTimeout(ctx, 10*time.Second)
				err := processWithDecision(attempt, pool, record, decide, log)
				if err == nil {
					err = client.CommitRecords(attempt, record)
				}
				cancel()
				if err == nil {
					log.Info("notification event acknowledged", "topic", record.Topic, "partition", record.Partition, "offset", record.Offset)
					break
				}
				log.Warn("notification event retry", "error", err)
				timer := time.NewTimer(time.Second)
				select {
				case <-ctx.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
			}
		}
		client.AllowRebalance()
	}
}
