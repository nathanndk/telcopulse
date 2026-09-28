package store

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"telcopulse/services/shared/domain"
)

func TestAuditFilterAndSafeProjection(t *testing.T) {
	f := AuditFilter{Environment: "development", Limit: 25}
	if _, err := auditBoundary(f); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []AuditFilter{{Environment: "production", Limit: 25}, {Environment: "development", Source: "auth", Limit: 25}, {Environment: "development", Limit: 101}, {Environment: "development", Limit: 25, Cursor: "bad"}} {
		if _, err := auditBoundary(invalid); !errors.Is(err, ErrInvalidAuditFilter) {
			t.Fatalf("accepted invalid audit filter %+v: %v", invalid, err)
		}
	}
	before := []byte(`{"state":"Detected","owner":"Alice","impact":"subscriber 628123456789"}`)
	after := []byte(`{"state":"Investigating","owner":"Bob","impact":"subscriber 628999999999"}`)
	changes, err := safeAuditChanges(before, after)
	if err != nil || len(changes) != 3 || changes[0].Field != "state" || changes[0].Before != "Detected" || changes[0].After != "Investigating" {
		t.Fatalf("unexpected changes: %+v %v", changes, err)
	}
	raw, _ := json.Marshal(changes)
	if strings.Contains(string(raw), "628") || !strings.Contains(string(raw), `"field":"impact"`) {
		t.Fatalf("free text leaked or change omitted: %s", raw)
	}
}

func TestAuditEventsAcrossSources(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL required for audit integration")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	suffix, err := domain.NewID(12)
	if err != nil {
		t.Fatal(err)
	}
	actor := "audit-test-" + suffix
	incidentID := "INC-" + suffix
	simulationID := "SIM-" + suffix
	deploymentID := "DEP-" + suffix
	base := time.Date(2030, 1, 2, 3, 0, 0, 0, time.UTC)
	insert := func(query string, args ...any) {
		t.Helper()
		if _, err := tx.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	insert(`INSERT INTO incident.records(id,creation_key,request_hash,environment,state,version,document,updated_at,detected_at)
VALUES($1,$2,'audit-test','development','Investigating',1,$3,$4,$4)`, incidentID, "incident-"+suffix,
		map[string]any{"id": incidentID, "environment": "development", "state": "Investigating", "version": 1, "detected_at": base}, base)
	insert(`INSERT INTO incident.audit(incident_id,version,entry) VALUES($1,1,$2)`, incidentID,
		map[string]any{"actor": actor, "action": "updated", "note": "subscriber 628123456789", "at": base.Add(time.Minute),
			"before": map[string]any{"state": "Detected", "impact": "subscriber 628123456789"},
			"after":  map[string]any{"state": "Investigating", "impact": "subscriber 628999999999"}})
	insert(`INSERT INTO simulation.runs(id,creation_key,request,environment,percentage,started_at,expires_at,reason)
VALUES($1,$2,'{}','development',10,$3,$4,'audit-test')`, simulationID, "simulation-"+suffix, base, base.Add(time.Hour))
	insert(`INSERT INTO simulation.audit(run_id,action,actor,note,at) VALUES($1,'started',$2,'private simulation note',$3)`, simulationID, actor, base.Add(2*time.Minute))
	insert(`INSERT INTO deployment.records(id,service,environment,version,commit_sha,deployer,status,occurred_at)
VALUES($1,'payment-service','development','test','abcdef0',$2,'Completed',$3)`, deploymentID, actor, base)
	insert(`INSERT INTO deployment.events(event_id,deployment_id,request_hash,actor,status,occurred_at,recorded_at)
VALUES($1,$2,'audit-test',$3,'Completed',$4,$4)`, "event-"+suffix, deploymentID, actor, base.Add(3*time.Minute))
	insert(`INSERT INTO simulation.runs(id,creation_key,request,environment,percentage,started_at,expires_at,reason)
VALUES($1,$2,'{}','staging',10,$3,$4,'audit-test')`, "SIM-staging-"+suffix, "staging-"+suffix, base, base.Add(time.Hour))
	insert(`INSERT INTO simulation.audit(run_id,action,actor,note,at) VALUES($1,'started',$2,'private',$3)`, "SIM-staging-"+suffix, actor, base.Add(4*time.Minute))

	f := AuditFilter{Environment: "development", Actor: actor, Limit: 2}
	page, err := auditEventsTx(ctx, tx, f, auditCursor{})
	if err != nil || len(page.Items) != 2 || !page.More || page.Next == "" || page.Items[0].Source != "deployment" || page.Items[1].Source != "simulation" {
		t.Fatalf("first audit page: %+v %v", page, err)
	}
	if page.Items[0].Note != "" || page.Items[1].Note != "" {
		t.Fatalf("private notes left source records: %+v", page.Items)
	}
	f.Cursor = page.Next
	boundary, err := auditBoundary(f)
	if err != nil {
		t.Fatal(err)
	}
	page, err = auditEventsTx(ctx, tx, f, boundary)
	if err != nil || len(page.Items) != 1 || page.More || page.Items[0].Source != "incident" || page.Items[0].ResourceID != incidentID {
		t.Fatalf("second audit page: %+v %v", page, err)
	}
	raw, _ := json.Marshal(page)
	if strings.Contains(string(raw), "628") || !strings.Contains(string(raw), `"field":"impact"`) {
		t.Fatalf("incident payload leaked or safe change missing: %s", raw)
	}
	f.Source = "incident"
	if _, err := auditBoundary(f); !errors.Is(err, ErrInvalidAuditFilter) {
		t.Fatalf("cursor accepted for changed source: %v", err)
	}
	f.Cursor = ""
	f.Resource = incidentID
	page, err = auditEventsTx(ctx, tx, f, auditCursor{})
	if err != nil || len(page.Items) != 1 || page.Items[0].ResourceID != incidentID {
		t.Fatalf("source/resource filter: %+v %v", page, err)
	}
	f.Environment = "staging"
	f.Source, f.Resource = "", ""
	page, err = auditEventsTx(ctx, tx, f, auditCursor{})
	if err != nil || len(page.Items) != 1 || page.Items[0].Environment != "staging" {
		t.Fatalf("environment isolation: %+v %v", page, err)
	}
}
