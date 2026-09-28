package events

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"telcopulse/services/shared/domain"
)

// Outbox identifies a service-owned table; only these constants can become SQL identifiers.
type Outbox string

const (
	GatewayOutbox      Outbox = "public.event_outbox"
	PaymentOutbox      Outbox = "payment.event_outbox"
	NotificationOutbox Outbox = "notification.event_outbox"
	PaymentTopic              = "telcopulse.payment.state.v1"
	NotificationTopic         = "telcopulse.notification.delivered.v1"
)

func (o Outbox) valid() bool {
	return o == GatewayOutbox || o == PaymentOutbox || o == NotificationOutbox
}

// Change is an immutable domain fact. Sequence orders facts for one producer and
// transaction; downstream consumers must not assume cross-topic delivery order.
type Change struct {
	ID          string           `json:"event_id"`
	Version     int              `json:"schema_version"`
	Type        string           `json:"type"`
	OccurredAt  time.Time        `json:"occurred_at"`
	Operation   domain.Operation `json:"operation"`
	State       string           `json:"state"`
	Sequence    int              `json:"sequence"`
	ErrorCode   string           `json:"error_code,omitempty"`
	CausationID string           `json:"causation_id,omitempty"`
}

// EnqueuePayment records a state transition inside the payment transaction.
func EnqueuePayment(ctx context.Context, tx pgx.Tx, op domain.Operation, result domain.OperationResult) error {
	if !slices.Contains([]string{"RESERVED", "FAILED", "CONFIRMED", "RELEASED"}, result.Status) {
		return errors.New("invalid payment event state")
	}
	sequence := 1
	if result.Status == "CONFIRMED" || result.Status == "RELEASED" {
		sequence = 2
	}
	return enqueueChange(ctx, tx, PaymentOutbox, Change{ID: "payment.state:" + op.TransactionID + ":" + result.Status, Version: 1, Type: PaymentTopic, OccurredAt: time.Now().UTC(), Operation: op, State: result.Status, Sequence: sequence, ErrorCode: result.ErrorCode})
}

// EnqueueDelivery records the synthetic receipt in the same transaction as delivery.
func EnqueueDelivery(ctx context.Context, tx pgx.Tx, purchase PurchaseCompleted) error {
	if err := purchase.Validate(); err != nil {
		return err
	}
	return enqueueChange(ctx, tx, NotificationOutbox, Change{ID: "notification.delivered:" + purchase.Operation.TransactionID, Version: 1, Type: NotificationTopic, OccurredAt: time.Now().UTC(), Operation: purchase.Operation, State: "DELIVERED", Sequence: 1, CausationID: purchase.ID})
}
func enqueueChange(ctx context.Context, tx pgx.Tx, outbox Outbox, event Change) error {
	if !outbox.valid() || !domain.ValidOperation(event.Operation) {
		return errors.New("invalid domain event")
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, "INSERT INTO "+string(outbox)+"(event_id,topic,partition_key,payload,traceparent) VALUES($1,$2,$3,$4,$5)", event.ID, event.Type, event.Operation.TransactionID, payload, TraceParent(ctx))
	return err
}
