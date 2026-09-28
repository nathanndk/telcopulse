package incident

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"telcopulse/services/shared/domain"
)

func TestPurchaseImpactValidation(t *testing.T) {
	store := Store{}
	i := Incident{ID: "INC-0123456789abcdef01234567", Environment: "development", Service: "notification-service", DetectedAt: time.Now().UTC().Add(-time.Hour)}
	result, err := store.PurchaseImpact(context.Background(), i, "2h", "")
	if err != nil || result.Applicable || len(result.Items) != 0 {
		t.Fatalf("unsupported service: %+v %v", result, err)
	}
	if _, err = store.PurchaseImpact(context.Background(), i, "bad", ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad window: %v", err)
	}
	if _, err = store.PurchaseImpact(context.Background(), i, "2h", "bad"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unsupported cursor: %v", err)
	}
	i.Service = "payment-service"
	if _, err = store.PurchaseImpact(context.Background(), i, "2h", "bad"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad cursor: %v", err)
	}
}

func TestPurchaseImpactCountsAndPages(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL required for purchase impact integration")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	store := Store{Pool: pool}
	suffix, err := domain.NewID(12)
	if err != nil {
		t.Fatal(err)
	}
	detected := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	incident := Incident{ID: "INC-" + suffix, Environment: "development", Service: "payment-service", DetectedAt: detected}
	ids := []string{}
	insert := func(env, status, customer string, at time.Time) string {
		t.Helper()
		idPart, e := domain.NewID(12)
		if e != nil {
			t.Fatal(e)
		}
		id := "TXN-" + idPart
		_, e = pool.Exec(ctx, `INSERT INTO transactions(id,trace_id,idempotency_key,request_hash,customer_id,package_id,environment,status,created_at,result)
   VALUES($1,$2,$3,'impact-test',$4,'pkg-3',$5,$6,$7,$8)`, id, "trace-"+idPart, "impact-"+idPart, customer, env, status, at, `{"error_code":"DB_TIMEOUT"}`)
		if e != nil {
			t.Fatal(e)
		}
		ids = append(ids, id)
		return id
	}
	t.Cleanup(func() {
		for _, id := range ids {
			_, _ = pool.Exec(ctx, `DELETE FROM transactions WHERE id=$1`, id)
		}
	})
	newest := ""
	for n := 0; n < 21; n++ {
		id := insert("development", "FAILED", "cus-001", detected.Add(time.Duration(n+1)*time.Minute))
		if n == 20 {
			newest = id
		}
	}
	insert("development", "SUCCESS", "cus-002", detected.Add(25*time.Minute))
	insert("staging", "FAILED", "cus-002", detected.Add(26*time.Minute))
	insert("development", "FAILED", "cus-002", detected.Add(3*time.Hour))
	first, err := store.PurchaseImpact(ctx, incident, "2h", "")
	if err != nil || first.Total != 22 || first.Success != 1 || first.Failed != 21 || first.FailedCustomers != 1 || len(first.Items) != 20 || !first.More || first.Next == "" || first.Items[0].ID != newest {
		t.Fatalf("impact cohort: %+v %v", first, err)
	}
	next, err := store.PurchaseImpact(ctx, incident, "2h", first.Next)
	if err != nil || len(next.Items) != 1 || next.More || next.Items[0].ErrorCode != "DB_TIMEOUT" {
		t.Fatalf("second page: %+v %v", next, err)
	}
	if _, err = store.PurchaseImpact(ctx, incident, "24h", first.Next); !errors.Is(err, ErrInvalid) {
		t.Fatalf("cross-window cursor: %v", err)
	}
	incident.Service = "notification-service"
	unsupported, err := store.PurchaseImpact(ctx, incident, "2h", "")
	if err != nil || unsupported.Applicable {
		t.Fatalf("unsupported service: %+v %v", unsupported, err)
	}
}
