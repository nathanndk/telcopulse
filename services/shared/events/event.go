// Package events defines versioned integration events and durable Kafka publication.
package events

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"telcopulse/services/shared/domain"
)

// PurchaseTopic carries completed business outcomes, including failures.
const PurchaseTopic = "telcopulse.purchase.completed.v1"

// DeadLetterTopic carries redacted poison-record metadata, never raw payloads.
const DeadLetterTopic = "telcopulse.notification.dead-letter.v1"

// PurchaseCompleted contains no raw subscriber PII. ID is stable across redelivery.
type PurchaseCompleted struct {
	ID         string           `json:"event_id"`
	Version    int              `json:"schema_version"`
	Type       string           `json:"type"`
	OccurredAt time.Time        `json:"occurred_at"`
	Operation  domain.Operation `json:"operation"`
}

// Validate rejects incompatible or incomplete messages before any delivery side effect.
func (e PurchaseCompleted) Validate() error {
	if e.ID != "purchase.completed:"+e.Operation.TransactionID || e.Version != 1 || e.Type != PurchaseTopic || e.OccurredAt.IsZero() || !domain.ValidOperation(e.Operation) || (e.Operation.Outcome != "SUCCESS" && e.Operation.Outcome != "FAILED") {
		return errors.New("invalid purchase event schema or identity")
	}
	return nil
}

// Enqueue joins the caller's transaction: business outcome and event cannot diverge.
func Enqueue(ctx context.Context, tx pgx.Tx, e PurchaseCompleted) error {
	if err := e.Validate(); err != nil {
		return err
	}
	payload, err := json.Marshal(e)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO event_outbox(event_id,topic,partition_key,payload,traceparent) VALUES($1,$2,$3,$4,$5)`, e.ID, e.Type, e.Operation.TransactionID, payload, TraceParent(ctx))
	return err
}
