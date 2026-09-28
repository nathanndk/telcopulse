package workflow

import (
	"context"
	"testing"
	"time"

	"telcopulse/services/shared/domain"
)

func TestPurchaseSLOUsesCompletedEnvironmentWindow(t *testing.T) {
	s := setup(t, nil, nil, nil)
	ctx := context.Background()
	initial, err := s.Overview(ctx, "staging")
	if err != nil || initial.PurchaseSLO.Total != 0 || initial.PurchaseSLO.CurrentPercent != nil {
		t.Fatalf("empty SLO: %+v %v", initial.PurchaseSLO, err)
	}
	insert := func(id, env, status string, at time.Time) {
		t.Helper()
		_, err := s.Pool.Exec(ctx, `INSERT INTO transactions(id,trace_id,idempotency_key,request_hash,customer_id,package_id,environment,status,created_at,result) VALUES($1,$2,$3,'test','cus-001','pkg-10',$4,$5,$6,'{"duration_ms":1}')`, id, id+"-trace", id+"-key", env, status, at)
		if err != nil {
			t.Fatal(err)
		}
	}
	insert("slo-staging-ok", "staging", "SUCCESS", time.Now().Add(-time.Hour))
	insert("slo-staging-failed", "staging", "FAILED", time.Now().Add(-time.Hour))
	insert("slo-development-failed", "development", "FAILED", time.Now().Add(-time.Hour))
	insert("slo-staging-old", "staging", "FAILED", time.Now().Add(-31*24*time.Hour))
	out, err := s.Overview(ctx, "staging")
	if err != nil {
		t.Fatal(err)
	}
	slo := out.PurchaseSLO
	if slo.Total != 2 || slo.Success != 1 || slo.Failed != 1 || slo.CurrentPercent == nil || *slo.CurrentPercent != 50 || slo.BudgetRemaining != 0 || slo.BudgetConsumedPercent == nil || *slo.BudgetConsumedPercent < 100 {
		t.Fatalf("SLO includes the wrong transactions: %+v", slo)
	}
	if duration := slo.WindowEnd.Sub(slo.WindowStart); duration != 30*24*time.Hour {
		t.Fatalf("unexpected window: %s", duration)
	}
	if _, _, err := s.Purchase(ctx, domain.Purchase{CustomerID: "cus-001", PackageID: "pkg-10", PaymentMethod: "E-Wallet", Environment: "staging"}, "slo-synthetic-purchase-key"); err != nil {
		t.Fatal(err)
	}
	again, err := s.Overview(ctx, "staging")
	if err != nil || again.PurchaseSLO.Total != 3 || again.PurchaseSLO.Success != 2 {
		t.Fatalf("new committed purchase missing from SLO: %+v %v", again.PurchaseSLO, err)
	}
}
