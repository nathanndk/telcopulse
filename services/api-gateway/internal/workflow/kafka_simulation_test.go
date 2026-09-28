package workflow

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	payment "telcopulse/services/payment-service"
	"telcopulse/services/shared/faults"
	"telcopulse/services/shared/rpc"
	rt "telcopulse/services/shared/runtime"
	simulation "telcopulse/services/simulation-service"
	"testing"
	"time"
)

func TestKafkaSimulationPurchaseAndStop(t *testing.T) {
	s := setup(t, nil, nil, nil)
	ctx := context.Background()
	repo := simulation.Store{Pool: s.Pool}
	key := time.Now().Format("20060102150405.000000000")
	run, _, err := repo.Create(ctx, simulation.Create{Environment: "development", Scenario: "kafka-consumer-lag", Percentage: 100, DurationSeconds: 30, DelayMS: 4200, Reason: "Verify asynchronous slowdown"}, key)
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
	result, _, err := s.Purchase(ctx, purchaseInput(), key+"purchase")
	if err != nil || result.Status != "SUCCESS" {
		t.Fatalf("purchase %+v %v", result, err)
	}
	in := faults.Request{TransactionID: result.ID, Environment: "development"}
	decision, err := repo.Decide(ctx, in)
	if err != nil || !decision.Inject || !decision.Active || decision.Scenario != "kafka-consumer-lag" {
		t.Fatalf("decision %+v %v", decision, err)
	}
	if _, err = repo.Stop(ctx, run.ID, "Drain pending events"); err != nil {
		t.Fatal(err)
	}
	stopped, err := repo.Decide(ctx, in)
	if err != nil || stopped.Active || !stopped.Inject || stopped.RunID != decision.RunID || stopped.DelayMS != 4200 {
		t.Fatalf("stopped decision %+v %v", stopped, err)
	}
}
