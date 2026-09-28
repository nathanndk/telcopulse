package notification

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"telcopulse/services/shared/events"
)

var ErrReplayConflict = errors.New("replay key belongs to a different command")

type ReplayResult struct {
	SourceTopic     string    `json:"source_topic"`
	SourcePartition int32     `json:"source_partition"`
	SourceOffset    int64     `json:"source_offset"`
	Status          string    `json:"status"`
	Reason          string    `json:"reason"`
	TransactionID   string    `json:"transaction_id"`
	Actor           string    `json:"actor"`
	AttemptedAt     time.Time `json:"attempted_at"`
}

// Replay revalidates a retained purchase event under the current consumer code.
// It commits any new receipt and an immutable operator audit in one transaction.
func Replay(ctx context.Context, pool *pgxpool.Pool, partition int32, offset int64, key, actor string) (ReplayResult, bool, error) {
	result := ReplayResult{SourceTopic: events.PurchaseTopic, SourcePartition: partition, SourceOffset: offset, Actor: actor}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return result, false, err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if e := tx.Rollback(cleanup); e != nil && !errors.Is(e, pgx.ErrTxClosed) {
			slog.Error("release replay transaction", "error", e)
		}
	}()
	// A transaction-scoped key lock serializes uncertain client retries. The
	// source row lock also serializes distinct keys for the same poison record.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, key); err != nil {
		return result, false, err
	}
	var payload []byte
	err = tx.QueryRow(ctx, `SELECT payload FROM notification.dead_letters WHERE topic=$1 AND partition_id=$2 AND message_offset=$3 FOR UPDATE`, events.PurchaseTopic, partition, offset).Scan(&payload)
	if err != nil {
		return result, false, err
	}
	var prior ReplayResult
	err = tx.QueryRow(ctx, `SELECT topic,partition_id,message_offset,actor,status,reason,transaction_id,attempted_at FROM notification.dead_letter_replays WHERE idempotency_key=$1`, key).
		Scan(&prior.SourceTopic, &prior.SourcePartition, &prior.SourceOffset, &prior.Actor, &prior.Status, &prior.Reason, &prior.TransactionID, &prior.AttemptedAt)
	if err == nil {
		if prior.SourceTopic != result.SourceTopic || prior.SourcePartition != partition || prior.SourceOffset != offset || prior.Actor != actor {
			return result, false, ErrReplayConflict
		}
		return prior, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return result, false, err
	}
	if _, err = tx.Exec(ctx, `SAVEPOINT replay_delivery`); err != nil {
		return result, false, err
	}
	transactionID, created, deliverErr := deliverInto(ctx, tx, payload)
	var invalid *invalidEvent
	if errors.As(deliverErr, &invalid) {
		if _, err = tx.Exec(ctx, `ROLLBACK TO SAVEPOINT replay_delivery`); err != nil {
			return result, false, err
		}
		result.Status, result.Reason = "REJECTED", invalid.reason
	} else if deliverErr != nil {
		return result, false, deliverErr
	} else {
		result.TransactionID = transactionID
		result.Status = "ALREADY_DELIVERED"
		if created {
			result.Status = "DELIVERED"
		}
	}
	if _, err = tx.Exec(ctx, `RELEASE SAVEPOINT replay_delivery`); err != nil {
		return result, false, err
	}
	err = tx.QueryRow(ctx, `INSERT INTO notification.dead_letter_replays(idempotency_key,topic,partition_id,message_offset,actor,status,reason,transaction_id)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING attempted_at`, key, events.PurchaseTopic, partition, offset, actor, result.Status, result.Reason, result.TransactionID).Scan(&result.AttemptedAt)
	if err != nil {
		return result, false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return result, false, err
	}
	return result, false, nil
}
