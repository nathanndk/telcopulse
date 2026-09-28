package workflow

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	payment "telcopulse/services/payment-service"
	"telcopulse/services/shared/domain"
	"telcopulse/services/shared/faults"
	"telcopulse/services/shared/rpc"
	rt "telcopulse/services/shared/runtime"
	simulation "telcopulse/services/simulation-service"
	"testing"
	"time"
)

func TestDatabaseLatencyCancellationAndRecovery(t *testing.T) {
	s := setup(t, nil, nil, nil)
	ctx := context.Background()
	repo := simulation.Store{Pool: s.Pool}
	key := time.Now().Format("20060102150405.000000000")
	input := simulation.Create{Environment: "development", Scenario: "database-latency", Percentage: 100, DurationSeconds: 30, DelayMS: 180, Reason: "Verify actual database latency"}
	run, _, err := repo.Create(ctx, input, key)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, e := repo.Stop(ctx, run.ID, "Test cleanup"); e != nil {
			t.Error(e)
		}
	}()
	for _, ms := range []int{0, 99, 4501} {
		bad := input
		bad.DelayMS = ms
		if _, _, e := repo.Create(ctx, bad, key+"invalid"); e == nil {
			t.Fatal("invalid delay accepted")
		}
	}
	log := slog.New(slog.NewJSONHandler(io.Discard, nil))
	token := "integration-only-service-token-not-a-real-secret"
	server := httptest.NewServer(rt.Protect(payment.HandlerWithDecision(s.Pool, log, repo.Decide), token))
	defer server.Close()
	s.Payment = rpc.New(server.URL, token)
	start := time.Now()
	result, _, err := s.Purchase(ctx, purchaseInput(), key+"purchase")
	if err != nil || result.Status != "SUCCESS" || time.Since(start) < 150*time.Millisecond {
		t.Fatalf("delayed purchase %+v %v", result, err)
	}
	detail, err := repo.Get(ctx, run.ID, "")
	if err != nil || detail.Run.DelayMS != 180 || detail.Run.Scenario != "database-latency" || detail.Selected != 1 {
		t.Fatalf("detail %+v %v", detail, err)
	}
	op := domain.Operation{TransactionID: "TXN-" + run.ID + "cancel", TraceID: "01234567890123456789012345678901", CustomerID: "cus-001", PackageID: "pkg-10", Environment: "development", PaymentMethod: "E-Wallet", Amount: 100, Days: 1}
	short, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	var out domain.OperationResult
	err = s.Payment.Call(short, http.MethodPost, "/internal/reserve", op, &out, op.TraceID, op.TransactionID)
	cancel()
	if err == nil {
		t.Fatal("expected cancellation")
	}
	// A retry obtains the same transaction lock, so it also waits for cancellation rollback.
	start = time.Now()
	err = s.Payment.Call(ctx, http.MethodPost, "/internal/reserve", op, &out, op.TraceID, op.TransactionID)
	if err != nil || out.Status != "RESERVED" || time.Since(start) < 150*time.Millisecond {
		t.Fatalf("cancel retry %+v %v", out, err)
	}
	// Once persisted, replay must not consult simulation or delay again.
	fast := httptest.NewServer(rt.Protect(payment.HandlerWithDecision(s.Pool, log, func(context.Context, faults.Request) (faults.Decision, error) {
		t.Error("replayed reservation requested another decision")
		return faults.Decision{}, nil
	}), token))
	defer fast.Close()
	if err = rpc.New(fast.URL, token).Call(ctx, http.MethodPost, "/internal/reserve", op, &out, op.TraceID, op.TransactionID); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.Stop(ctx, run.ID, "Restore normal latency"); err != nil {
		t.Fatal(err)
	}
	recovered, _, err := s.Purchase(ctx, purchaseInput(), key+"recovered")
	if err != nil || recovered.Status != "SUCCESS" {
		t.Fatalf("recovery %+v %v", recovered, err)
	}
}
