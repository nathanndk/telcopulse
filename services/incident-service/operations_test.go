package incident

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"telcopulse/services/shared/domain"
)

func TestIncidentOperationsWindowAndDenominators(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL required for incident operations integration")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	store := Store{Pool: pool}
	empty, err := store.Operations(ctx, "staging")
	if err != nil || empty.IncidentCount != 0 || empty.MTTASeconds != nil || empty.MTTRSeconds != nil {
		t.Fatalf("empty window: %+v %v", empty, err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	insert := func(env, service, root, severity string, detected time.Time, ack, resolved *time.Time) string {
		t.Helper()
		suffix, err := domain.NewID(12)
		if err != nil {
			t.Fatal(err)
		}
		id := "INC-" + suffix
		state := Detected
		if resolved != nil {
			state = Resolved
		} else if ack != nil {
			state = Acknowledged
		}
		item := Incident{Fields: Fields{Title: "Operations fixture", Severity: severity, RootCause: root}, ID: id, Environment: env, Service: service, State: state, Version: 1, CreatedAt: now, DetectedAt: detected, UpdatedAt: now, AcknowledgedAt: ack, ResolvedAt: resolved}
		document, err := json.Marshal(item)
		if err != nil {
			t.Fatal(err)
		}
		_, err = pool.Exec(ctx, `INSERT INTO incident.records(id,creation_key,request_hash,environment,state,version,document,updated_at) VALUES($1,$2,'ops-test',$3,$4,1,$5,$6)`, id, "ops-"+suffix, env, state, document, now)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	detectedA := now.Add(-2 * time.Hour)
	ackA := detectedA.Add(10 * time.Minute)
	resolvedA := detectedA.Add(time.Hour)
	a := insert("staging", "payment-service", "Pool exhaustion", "SEV-1", detectedA, &ackA, &resolvedA)
	detectedB := now.Add(-time.Hour)
	ackB := detectedB.Add(10 * time.Minute)
	b := insert("staging", "payment-service", " pool EXHAUSTION ", "SEV-2", detectedB, &ackB, nil)
	insert("staging", "package-service", "Cache key", "SEV-3", now.Add(-45*time.Minute), nil, nil)
	insert("staging", "payment-service", "", "SEV-4", now.Add(-30*time.Minute), nil, nil)
	insert("development", "payment-service", "Pool exhaustion", "SEV-1", now.Add(-20*time.Minute), nil, nil)
	insert("staging", "payment-service", "Pool exhaustion", "SEV-1", now.Add(-31*24*time.Hour), nil, nil)
	for index, id := range []string{a, b, b} {
		at := now.Add(-time.Duration(index+1) * time.Minute)
		entry, err := json.Marshal(Audit{Version: int64(index + 2), Action: "escalated", Actor: "test", At: at})
		if err != nil {
			t.Fatal(err)
		}
		_, err = pool.Exec(ctx, `INSERT INTO incident.audit(incident_id,version,entry) VALUES($1,$2,$3)`, id, index+2, entry)
		if err != nil {
			t.Fatal(err)
		}
	}
	out, err := store.Operations(ctx, "staging")
	if err != nil {
		t.Fatal(err)
	}
	if out.IncidentCount != 4 || out.SEV1Count != 1 || out.AcknowledgedCount != 2 || out.ResolvedCount != 1 || out.EscalationEvents != 3 || out.RepeatCount != 1 {
		t.Fatalf("incorrect operational counts: %+v", out)
	}
	if out.MTTASeconds == nil || math.Abs(*out.MTTASeconds-600) > 0.01 || out.MTTRSeconds == nil || math.Abs(*out.MTTRSeconds-3600) > 0.01 {
		t.Fatalf("incorrect response times: %+v", out)
	}
	if out.WindowEnd.Sub(out.WindowStart) != 30*24*time.Hour {
		t.Fatalf("incorrect window: %+v", out)
	}
	if _, err = store.Operations(ctx, "production"); !errors.Is(err, ErrInvalid) {
		t.Fatal(fmt.Errorf("accepted out-of-scope environment: %v", err))
	}
}
