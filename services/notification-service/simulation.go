package notification

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
	"telcopulse/services/shared/events"
	"telcopulse/services/shared/faults"
)

// DecisionFunc resolves a durable transaction decision and current run activity.
type DecisionFunc func(context.Context, faults.Request) (faults.Decision, error)

func delayRecord(ctx context.Context, record *kgo.Record, decide DecisionFunc, log *slog.Logger) error {
	if decide == nil {
		return nil
	}
	var event events.PurchaseCompleted
	decoder := json.NewDecoder(bytes.NewReader(record.Value))
	decoder.DisallowUnknownFields()
	// Invalid messages proceed to the existing durable dead-letter path.
	if len(record.Value) > 16384 || decoder.Decode(&event) != nil || decoder.Decode(&struct{}{}) != io.EOF || event.Validate() != nil {
		return nil
	}
	decision, err := decide(ctx, faults.Request{TransactionID: event.Operation.TransactionID, Environment: event.Operation.Environment})
	if err != nil {
		return err
	}
	if !decision.Inject || !decision.Active || decision.Scenario != "kafka-consumer-lag" {
		return nil
	}
	if decision.DelayMS < 100 || decision.DelayMS > 4500 {
		return errors.New("invalid consumer simulation delay")
	}
	log.Warn("synthetic notification delay selected", "simulation_id", decision.RunID, "transaction_id", event.Operation.TransactionID, "environment", event.Operation.Environment, "delay_ms", decision.DelayMS)
	timer := time.NewTimer(time.Duration(decision.DelayMS) * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
