package notification

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"telcopulse/services/shared/events"
)

var ErrInvalidReplayCursor = errors.New("invalid dead-letter replay cursor")
var replayDigestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

type ReplayPage struct {
	Items []ReplayResult `json:"items"`
	More  bool           `json:"more"`
	Next  string         `json:"next,omitempty"`
}

type replayCursor struct {
	Partition int32     `json:"partition"`
	Offset    int64     `json:"offset"`
	At        time.Time `json:"at"`
	Digest    string    `json:"digest"`
}

// ReplayHistory returns only the append-only outcome metadata for one retained
// source. The cursor contains a digest, never an idempotency key.
func ReplayHistory(ctx context.Context, pool *pgxpool.Pool, partition int32, offset int64, limit int, rawCursor string) (ReplayPage, error) {
	page := ReplayPage{Items: []ReplayResult{}}
	if partition < 0 || offset < 0 || limit < 1 || limit > 50 || len(rawCursor) > 1024 {
		return page, ErrInvalidReplayCursor
	}
	var at any
	var digest any
	if rawCursor != "" {
		decoded, err := base64.RawURLEncoding.DecodeString(rawCursor)
		var cursor replayCursor
		if err == nil {
			decoder := json.NewDecoder(bytes.NewReader(decoded))
			decoder.DisallowUnknownFields()
			err = decoder.Decode(&cursor)
			if err == nil && decoder.Decode(&struct{}{}) != io.EOF {
				err = ErrInvalidReplayCursor
			}
		}
		if err != nil || cursor.Partition != partition || cursor.Offset != offset || cursor.At.IsZero() || !replayDigestPattern.MatchString(cursor.Digest) {
			return page, ErrInvalidReplayCursor
		}
		at, digest = cursor.At, cursor.Digest
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return page, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var present int
	err = tx.QueryRow(ctx, `SELECT 1 FROM notification.dead_letters WHERE topic=$1 AND partition_id=$2 AND message_offset=$3`, events.PurchaseTopic, partition, offset).Scan(&present)
	if err != nil {
		return page, err
	}
	rows, err := tx.Query(ctx, `SELECT encode(sha256(convert_to(idempotency_key,'UTF8')),'hex'),actor,status,reason,transaction_id,attempted_at
 FROM notification.dead_letter_replays
 WHERE topic=$1 AND partition_id=$2 AND message_offset=$3
   AND ($4::timestamptz IS NULL OR (attempted_at,encode(sha256(convert_to(idempotency_key,'UTF8')),'hex'))<($4::timestamptz,$5::text))
 ORDER BY attempted_at DESC,encode(sha256(convert_to(idempotency_key,'UTF8')),'hex') DESC LIMIT $6`, events.PurchaseTopic, partition, offset, at, digest, limit+1)
	if err != nil {
		return page, err
	}
	lastDigest := ""
	for rows.Next() {
		var item ReplayResult
		var rowDigest string
		if err = rows.Scan(&rowDigest, &item.Actor, &item.Status, &item.Reason, &item.TransactionID, &item.AttemptedAt); err != nil {
			break
		}
		item.SourceTopic, item.SourcePartition, item.SourceOffset = events.PurchaseTopic, partition, offset
		page.Items = append(page.Items, item)
		if len(page.Items) == limit {
			lastDigest = rowDigest
		}
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return ReplayPage{}, err
	}
	if len(page.Items) > limit {
		page.More = true
		page.Items = page.Items[:limit]
		cursor, encodeErr := json.Marshal(replayCursor{Partition: partition, Offset: offset, At: page.Items[limit-1].AttemptedAt, Digest: lastDigest})
		if encodeErr != nil {
			return ReplayPage{}, encodeErr
		}
		page.Next = base64.RawURLEncoding.EncodeToString(cursor)
	}
	return page, tx.Commit(ctx)
}
