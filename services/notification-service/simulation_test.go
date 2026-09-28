package notification

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
	"telcopulse/services/shared/domain"
	"telcopulse/services/shared/events"
	"telcopulse/services/shared/faults"
)

func TestConsumerDelay(t *testing.T) {
	op := domain.Operation{TransactionID: "TXN-012345678901234567890123", TraceID: "01234567890123456789012345678901", CustomerID: "cus-001", PackageID: "pkg-10", Environment: "staging", PaymentMethod: "E-Wallet", Amount: 100, Days: 1, Outcome: "SUCCESS"}
	event := events.PurchaseCompleted{ID: "purchase.completed:" + op.TransactionID, Version: 1, Type: events.PurchaseTopic, OccurredAt: time.Now(), Operation: op}
	data, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	record := &kgo.Record{Value: data}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	for _, tc := range []struct {
		name                 string
		active, inject       bool
		scenario             string
		delay                int
		wantDelay, wantError bool
	}{
		{"selected", true, true, "kafka-consumer-lag", 100, true, false},
		{"stopped", false, true, "kafka-consumer-lag", 100, false, false},
		{"unselected", true, false, "kafka-consumer-lag", 100, false, false},
		{"other scenario", true, true, "database-latency", 100, false, false},
		{"invalid delay", true, true, "kafka-consumer-lag", 4501, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			decide := func(_ context.Context, in faults.Request) (faults.Decision, error) {
				calls++
				if in.TransactionID != op.TransactionID || in.Environment != "staging" {
					t.Fatal(in)
				}
				return faults.Decision{Active: tc.active, Inject: tc.inject, Scenario: tc.scenario, DelayMS: tc.delay}, nil
			}
			start := time.Now()
			err := delayRecord(context.Background(), record, decide, log)
			if (err != nil) != tc.wantError || calls != 1 {
				t.Fatalf("calls=%d err=%v", calls, err)
			}
			if tc.wantDelay && time.Since(start) < 90*time.Millisecond {
				t.Fatal("delay skipped")
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = delayRecord(ctx, record, func(context.Context, faults.Request) (faults.Decision, error) {
		return faults.Decision{Active: true, Inject: true, Scenario: "kafka-consumer-lag", DelayMS: 4500}, nil
	}, log)
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	sentinel := errors.New("unavailable")
	err = delayRecord(context.Background(), record, func(context.Context, faults.Request) (faults.Decision, error) { return faults.Decision{}, sentinel }, log)
	if !errors.Is(err, sentinel) {
		t.Fatal("lookup error swallowed")
	}
	err = delayRecord(context.Background(), &kgo.Record{Value: []byte("invalid")}, func(context.Context, faults.Request) (faults.Decision, error) {
		t.Fatal("invalid event consulted simulation")
		return faults.Decision{}, nil
	}, log)
	if err != nil {
		t.Fatal("invalid event must reach dead-letter handler")
	}
}
