package incident

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"telcopulse/services/shared/domain"
)

func TestRecoveryAssessmentStates(t *testing.T) {
	now := time.Now().UTC()
	a := RecoveryAssessment{Applicable: true, EarlierStart: now.Add(-10 * time.Minute), MinimumOutcomes: recoveryMinimumOutcomes, TargetSuccessRate: .999, EarlierTotal: 5, EarlierSuccess: 5, RecentTotal: 5, RecentSuccess: 5}
	if assessmentStatus(a) != "not_monitoring" {
		t.Fatal("missing monitoring start treated as healthy")
	}
	a.MonitoringAt = &now
	if assessmentStatus(a) != "collecting" {
		t.Fatal("short monitoring period treated as healthy")
	}
	started := now.Add(-12 * time.Minute)
	a.MonitoringAt = &started
	if assessmentStatus(a) != "meets_target" {
		t.Fatal("complete successful windows rejected")
	}
	a.RecentFailed, a.RecentSuccess = 1, 4
	if assessmentStatus(a) != "still_failing" {
		t.Fatal("failed purchase treated as recovered")
	}
	a.RecentFailed, a.RecentSuccess, a.RecentTotal = 0, 0, 0
	if assessmentStatus(a) != "insufficient_traffic" {
		t.Fatal("idle window treated as recovered")
	}
	a.Applicable = false
	if assessmentStatus(a) != "not_applicable" {
		t.Fatal("unsupported service assessed")
	}
}

func TestRecoveryAssessmentUsesTerminalTimeAndEnvironment(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL required for recovery assessment integration")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })
	now := time.Date(2021, 4, 1, 12, 0, 0, 0, time.UTC)
	monitoringAt := now.Add(-15 * time.Minute)
	suffix, err := domain.NewID(12)
	if err != nil {
		t.Fatal(err)
	}
	id := "INC-" + suffix
	incident := Incident{ID: id, Environment: "development", Service: "payment-service", State: Monitoring, Version: 1, CreatedAt: monitoringAt, DetectedAt: monitoringAt.Add(-time.Minute), UpdatedAt: monitoringAt}
	document, err := json.Marshal(incident)
	if err != nil {
		t.Fatal(err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO incident.records(id,creation_key,request_hash,environment,state,version,document,updated_at) VALUES($1,$2,'recovery-test','development','Monitoring',1,$3,$4)`, id, "recovery-"+suffix, document, monitoringAt)
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	audit, err := json.Marshal(Audit{Version: 1, At: monitoringAt, Before: &Incident{State: Mitigating}, After: incident})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO incident.audit(incident_id,version,entry) VALUES($1,1,$2)`, id, audit); err != nil {
		t.Fatal(err)
	}
	insert := func(environment, status string, completed time.Time) string {
		t.Helper()
		part, e := domain.NewID(12)
		if e != nil {
			t.Fatal(e)
		}
		transactionID := "TXN-" + part
		key := "recovery-" + part
		_, e = tx.Exec(ctx, `INSERT INTO transactions(id,trace_id,idempotency_key,request_hash,customer_id,package_id,environment,status,created_at,result) VALUES($1,$2,$3,'recovery-test','cus-001','pkg-3',$4,$5,$6,'{}')`, transactionID, "trace-"+part, key, environment, status, now.Add(-30*time.Minute))
		if e != nil {
			t.Fatal(e)
		}
		ids = append(ids, transactionID)
		_, e = tx.Exec(ctx, `INSERT INTO purchase_workflows(idempotency_key,transaction_id,request,result,state,created_at,updated_at) VALUES($1,$2,'{}','{}',$3,$4,$5)`, key, transactionID, status, now.Add(-30*time.Minute), completed)
		if e != nil {
			t.Fatal(e)
		}
		return transactionID
	}
	for n := 0; n < 5; n++ {
		insert("development", "SUCCESS", now.Add(-7*time.Minute))
	}
	for n := 0; n < 5; n++ {
		insert("development", "SUCCESS", now.Add(-2*time.Minute))
	}
	insert("staging", "FAILED", now.Add(-2*time.Minute))
	a, err := assessRecoveryTx(ctx, tx, id, now)
	if err != nil || a.Status != "meets_target" || a.EarlierTotal != 5 || a.RecentTotal != 5 || a.EarlierFailed != 0 || a.RecentFailed != 0 || a.MonitoringAt == nil || !a.MonitoringAt.Equal(monitoringAt) {
		t.Fatalf("unexpected recovery sample: %+v %v", a, err)
	}
	if _, err = tx.Exec(ctx, `UPDATE transactions SET status='FAILED' WHERE id=$1`, ids[8]); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `UPDATE purchase_workflows SET state='FAILED',updated_at=$2 WHERE transaction_id=$1`, ids[8], now.Add(-2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	a, err = assessRecoveryTx(ctx, tx, id, now)
	if err != nil || a.Status != "still_failing" || a.RecentFailed != 1 {
		t.Fatalf("failure not reflected: %+v %v", a, err)
	}
	if _, err = tx.Exec(ctx, `UPDATE incident.records SET state='Investigating',document=jsonb_set(document,'{state}','"Investigating"'::jsonb) WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	a, err = assessRecoveryTx(ctx, tx, id, now)
	if err != nil || a.Status != "not_monitoring" {
		t.Fatalf("non-monitoring incident assessed: %+v %v", a, err)
	}
}
