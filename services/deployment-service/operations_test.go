package deployment

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"telcopulse/services/shared/domain"
)

func TestDeploymentOperationsCohortAndRollback(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL required for deployment operations integration")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	store := Store{Pool: pool}
	empty, err := store.Operations(ctx, "staging")
	if err != nil || empty.Evaluated != 0 || empty.FailureRatePercent != nil {
		t.Fatalf("empty deployment cohort: %+v %v", empty, err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	create := func(environment, status string, when time.Time) Event {
		t.Helper()
		suffix, err := domain.NewID(8)
		if err != nil {
			t.Fatal(err)
		}
		event := Event{EventID: "ops-event-" + suffix, DeploymentID: "ops-release-" + suffix, Service: "payment-service", Version: "1.2.3", CommitSHA: "abcdef0123456789abcdef0123456789abcdef01", Environment: environment, Deployer: "ci:jenkins", Status: status, OccurredAt: when}
		if _, _, err = store.Report(ctx, event); err != nil {
			t.Fatal(err)
		}
		return event
	}
	create("staging", "Completed", now.Add(-time.Hour))
	rolled := create("staging", "Completed", now.Add(-2*time.Hour))
	create("staging", "Failed", now.Add(-3*time.Hour))
	create("staging", "Pending", now.Add(-4*time.Hour))
	create("staging", "Completed", now.Add(-31*24*time.Hour))
	create("development", "Failed", now.Add(-time.Hour))
	rolled.EventID += "-rollback"
	rolled.Status = "Rolled Back"
	rolled.OccurredAt = now.Add(-30 * time.Minute)
	if _, _, err = store.Report(ctx, rolled); err != nil {
		t.Fatal(err)
	}
	out, err := store.Operations(ctx, "staging")
	if err != nil {
		t.Fatal(err)
	}
	if out.Evaluated != 3 || out.Failed != 2 || out.FailureRatePercent == nil || math.Abs(*out.FailureRatePercent-200.0/3) > 0.01 {
		t.Fatalf("incorrect deployment failure rate: %+v", out)
	}
	if out.WindowEnd.Sub(out.WindowStart) != 30*24*time.Hour {
		t.Fatalf("incorrect window: %+v", out)
	}
	if _, err = store.Operations(ctx, "other"); !errors.Is(err, ErrInvalid) {
		t.Fatal(fmt.Errorf("accepted invalid environment: %v", err))
	}
}
