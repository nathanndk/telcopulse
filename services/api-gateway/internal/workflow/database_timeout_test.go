package workflow

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	payment "telcopulse/services/payment-service"
	"telcopulse/services/shared/rpc"
	rt "telcopulse/services/shared/runtime"
	simulation "telcopulse/services/simulation-service"
	"testing"
	"time"
)

func TestDatabaseTimeoutFailureAndRecovery(t *testing.T) {
	s := setup(t, nil, nil, nil)
	ctx := context.Background()
	repo := simulation.Store{Pool: s.Pool}
	key := time.Now().Format("20060102150405.000000000")
	run, _, err := repo.Create(ctx, simulation.Create{Environment: "development", Scenario: "database-timeout", Percentage: 100, DurationSeconds: 30, DelayMS: 180, Reason: "Verify PostgreSQL timeout"}, key)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, e := repo.Stop(ctx, run.ID, "Cleanup"); e != nil {
			t.Error(e)
		}
	}()
	token := "integration-only-service-token-not-a-real-secret"
	server := httptest.NewServer(rt.Protect(payment.HandlerWithDecision(s.Pool, slog.New(slog.NewTextHandler(io.Discard, nil)), repo.Decide), token))
	defer server.Close()
	s.Payment = rpc.New(server.URL, token)
	var before, after int64
	if err = s.Pool.QueryRow(ctx, "SELECT balance_idr FROM payment.accounts WHERE customer_id='cus-001'").Scan(&before); err != nil {
		t.Fatal(err)
	}
	input := purchaseInput()
	input.PaymentMethod = "Pulsa"
	start := time.Now()
	result, _, err := s.Purchase(ctx, input, key+"purchase")
	if err != nil || result.Status != "FAILED" || result.ErrorCode != "DB_TIMEOUT" || time.Since(start) < 150*time.Millisecond {
		t.Fatalf("timeout %+v %v", result, err)
	}
	if err = s.Pool.QueryRow(ctx, "SELECT balance_idr FROM payment.accounts WHERE customer_id='cus-001'").Scan(&after); err != nil || before != after {
		t.Fatalf("balance before=%d after=%d error=%v", before, after, err)
	}
	var count int
	if err = s.Pool.QueryRow(ctx, "SELECT count(*) FROM payment.event_outbox WHERE partition_key=$1", result.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("outbox %d %v", count, err)
	}
	if _, err = repo.Stop(ctx, run.ID, "Restore queries"); err != nil {
		t.Fatal(err)
	}
	replay, _, err := s.Purchase(ctx, input, key+"purchase")
	if err != nil || replay.ID != result.ID || replay.ErrorCode != "DB_TIMEOUT" {
		t.Fatalf("replay %+v %v", replay, err)
	}
	recovered, _, err := s.Purchase(ctx, input, key+"recovered")
	if err != nil || recovered.Status != "SUCCESS" {
		t.Fatalf("recovery %+v %v", recovered, err)
	}
}
