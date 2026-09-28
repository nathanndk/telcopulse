package workflow

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http/httptest"
	"sync"
	payment "telcopulse/services/payment-service"
	"telcopulse/services/shared/faults"
	"telcopulse/services/shared/rpc"
	rt "telcopulse/services/shared/runtime"
	simulation "telcopulse/services/simulation-service"
	"testing"
	"time"
)

func TestSimulationDecisionsAndPurchaseRecovery(t *testing.T) {
	s := setup(t, nil, nil, nil)
	ctx := context.Background()
	repo := simulation.Store{Pool: s.Pool}
	input := simulation.Create{Environment: "development", Scenario: "payment-decline", Percentage: 100, DurationSeconds: 60, Reason: "Verify controlled decline"}
	key := time.Now().Format("20060102150405.000000000")
	startActor := "operator:USR-0123456789abcdef01234567"
	stopActor := "operator:USR-fedcba9876543210fedcba98"
	run, replayed, err := repo.CreateAs(ctx, input, key, startActor)
	if err != nil || replayed {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() {
		if _, e := repo.Stop(ctx, run.ID, "Test cleanup"); e != nil {
			t.Error(e)
		}
	})
	replay, replayed, err := repo.Create(ctx, input, key)
	if err != nil || !replayed || replay.ID != run.ID {
		t.Fatalf("replay: %v", err)
	}
	if _, _, err = repo.Create(ctx, input, key+"other"); !errors.Is(err, simulation.ErrConflict) {
		t.Fatalf("overlapping run: %v", err)
	}
	changed := input
	changed.Percentage = 50
	if _, _, err = repo.Create(ctx, changed, key); !errors.Is(err, simulation.ErrConflict) {
		t.Fatalf("changed key: %v", err)
	}
	for _, seconds := range []int{0, 29, 901} {
		invalid := input
		invalid.DurationSeconds = seconds
		if _, _, err = repo.Create(ctx, invalid, key); !errors.Is(err, simulation.ErrInvalid) {
			t.Fatalf("invalid duration: %v", err)
		}
	}
	identity := "TXN-" + run.ID
	isolated, err := repo.Decide(ctx, faults.Request{TransactionID: identity + "isolated", Environment: "staging"})
	if err != nil || isolated.Inject {
		t.Fatalf("environment isolation: %+v %v", isolated, err)
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d, e := repo.Decide(ctx, faults.Request{TransactionID: identity, Environment: "development"})
			if e != nil || !d.Inject || d.RunID != run.ID {
				t.Errorf("decision: %+v %v", d, e)
			}
		}()
	}
	wg.Wait()
	var n int
	if err = s.Pool.QueryRow(ctx, "SELECT count(*) FROM simulation.decisions WHERE transaction_id=$1", identity).Scan(&n); err != nil || n != 1 {
		t.Fatalf("decision dedup: %d %v", n, err)
	}
	log := slog.New(slog.NewJSONHandler(io.Discard, nil))
	token := "integration-only-service-token-not-a-real-secret"
	mutationToken := "simulation-mutation-test-token-123456"
	server := httptest.NewServer(rt.Protect(simulation.Handler(s.Pool, log, mutationToken), token))
	defer server.Close()
	client := rpc.New(server.URL, token)
	client.MutationToken = mutationToken
	paymentServer := httptest.NewServer(rt.Protect(payment.HandlerWithDecision(s.Pool, log, func(ctx context.Context, in faults.Request) (faults.Decision, error) {
		var d faults.Decision
		e := client.Call(ctx, "POST", "/internal/simulation/decision", in, &d, "", in.TransactionID)
		return d, e
	}), token))
	defer paymentServer.Close()
	s.Payment = rpc.New(paymentServer.URL, token)
	before := balance(t, s)
	failed, _, err := s.Purchase(ctx, purchaseInput(), key+"purchase1")
	if err != nil || failed.Status != "FAILED" || failed.ErrorCode != "SIMULATED_PAYMENT_DECLINED" {
		t.Fatalf("purchase: %+v %v", failed, err)
	}
	if balance(t, s) != before {
		t.Fatal("injected decline debited balance")
	}
	if count(t, s, "payment.event_outbox") != 1 {
		t.Fatal("missing durable payment event")
	}
	for n := 0; n < 25; n++ {
		if _, e := repo.Decide(ctx, faults.Request{TransactionID: fmt.Sprintf("%s-page-%02d", identity, n), Environment: "development"}); e != nil {
			t.Fatal(e)
		}
	}
	stopped, err := repo.StopAs(ctx, run.ID, "Mitigate synthetic outage", stopActor)
	if err != nil || stopped.Active || stopped.StoppedAt == nil {
		t.Fatalf("stop: %+v %v", stopped, err)
	}
	if _, err = repo.Stop(ctx, run.ID, "Repeat stop"); err != nil {
		t.Fatal(err)
	}
	if err = s.Pool.QueryRow(ctx, "SELECT count(*) FROM simulation.audit WHERE run_id=$1", run.ID).Scan(&n); err != nil || n != 2 {
		t.Fatalf("audit count %d %v", n, err)
	}
	detail, e := repo.Get(ctx, run.ID, "")
	if e != nil || detail.Observed != 27 || detail.Selected != 27 || len(detail.Audit) != 2 || detail.Audit[0].Actor != startActor || detail.Audit[1].Actor != stopActor || detail.Audit[1].Note != "Mitigate synthetic outage" || len(detail.Decisions) != 20 || !detail.More || detail.Run.Active {
		t.Fatalf("detail snapshot: %+v %v", detail, e)
	}
	second, e := repo.Get(ctx, run.ID, detail.Next)
	if e != nil || len(second.Decisions) != 7 || second.More {
		t.Fatalf("second page: %+v %v", second, e)
	}
	seen := map[string]bool{}
	for _, d := range append(detail.Decisions, second.Decisions...) {
		if seen[d.TransactionID] {
			t.Fatal("duplicate page entry")
		}
		seen[d.TransactionID] = true
	}
	if _, e = repo.Get(ctx, run.ID, "malformed"); !errors.Is(e, simulation.ErrInvalid) {
		t.Fatal("invalid cursor accepted")
	}
	if _, err = s.Pool.Exec(ctx, "UPDATE simulation.audit SET note='tampered' WHERE run_id=$1", run.ID); err == nil {
		t.Fatal("mutable audit")
	}
	frozen, err := repo.Decide(ctx, faults.Request{TransactionID: identity, Environment: "development"})
	if err != nil || !frozen.Inject {
		t.Fatal("stop changed old decision")
	}
	if _, err = repo.Decide(ctx, faults.Request{TransactionID: identity, Environment: "staging"}); !errors.Is(err, simulation.ErrConflict) {
		t.Fatal("transaction environment changed")
	}
	again, replayed, err := s.Purchase(ctx, purchaseInput(), key+"purchase1")
	if err != nil || !replayed || again.ID != failed.ID || again.Status != "FAILED" {
		t.Fatal("purchase retry changed result")
	}
	healthy, _, err := s.Purchase(ctx, purchaseInput(), key+"purchase2")
	if err != nil || healthy.Status != "SUCCESS" {
		t.Fatalf("recovery: %+v %v", healthy, err)
	}
	expired, _, err := repo.Create(ctx, input, key+"expiry")
	if err != nil {
		t.Fatal(err)
	}
	if _, e := repo.Get(ctx, expired.ID, detail.Next); !errors.Is(e, simulation.ErrInvalid) {
		t.Fatal("cross-run cursor accepted")
	}
	if _, err = s.Pool.Exec(ctx, "UPDATE simulation.runs SET started_at=$1,expires_at=$2 WHERE id=$3", time.Now().Add(-2*time.Minute), time.Now().Add(-time.Minute), expired.ID); err != nil {
		t.Fatal(err)
	}
	d, err := repo.Decide(ctx, faults.Request{TransactionID: identity + "expired", Environment: "development"})
	if err != nil || d.Inject {
		t.Fatal("expired run injected failure")
	}
	list, err := repo.List(ctx, "development")
	if err != nil || len(list) < 2 {
		t.Fatalf("list: %+v %v", list, err)
	}
	newRun, _, err := repo.Create(ctx, input, key+"new")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, e := repo.Stop(ctx, newRun.ID, "Test cleanup"); e != nil {
			t.Error(e)
		}
	}()
	d, err = repo.Decide(ctx, faults.Request{TransactionID: identity + "expired", Environment: "development"})
	if err != nil || d.Inject {
		t.Fatal("new run changed previous decision")
	}
}
