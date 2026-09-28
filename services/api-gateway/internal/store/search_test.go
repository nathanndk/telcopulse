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

func TestWorkspaceSearchValidation(t *testing.T) {
	for _, test := range []struct{ environment, query string }{
		{"production", "incident"}, {"development", "a"}, {"development", strings.Repeat("x", 81)},
		{"staging", "line\nbreak"}, {"development", "  "},
	} {
		if _, err := validateWorkspaceSearch(test.environment, test.query); !errors.Is(err, ErrInvalidWorkspaceSearch) {
			t.Fatalf("accepted invalid search %+v: %v", test, err)
		}
	}
	if query, err := validateWorkspaceSearch("development", "  payment  "); err != nil || query != "payment" {
		t.Fatalf("trim failed: %q %v", query, err)
	}
}

func TestWorkspaceSearchAcrossSources(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL required for workspace search integration")
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
	insert := func(query string, args ...any) {
		t.Helper()
		if _, err := tx.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	base := time.Date(2030, 2, 1, 0, 0, 0, 0, time.UTC)
	insert(`INSERT INTO customers(id,name,msisdn,balance_idr) VALUES($1,'Private customer',$2,100000)`, "cus-"+suffix, "test-"+suffix)
	insert(`INSERT INTO packages(id,name,data_gb,days,price_idr) VALUES($1,'Test package',1,1,1000)`, "pkg-"+suffix)
	insert(`INSERT INTO transactions(id,trace_id,idempotency_key,request_hash,customer_id,package_id,environment,status,created_at,result)
VALUES($1,$2,$3,'search-test',$4,$5,'development','FAILED',$6,$7)`, suffix, "trace-"+suffix, "key-"+suffix, "cus-"+suffix, "pkg-"+suffix, base,
		map[string]any{"msisdn": "628123456789", "customer_name": "Private customer"})
	insert(`INSERT INTO incident.records(id,creation_key,request_hash,environment,state,version,document,updated_at,detected_at)
VALUES($1,$2,'search-test','development','Detected',1,$3,$4,$4)`, "INC-"+suffix, "incident-search-"+suffix,
		map[string]any{"id": "INC-" + suffix, "environment": "development", "state": "Detected", "version": 1,
			"detected_at": base, "title": "Router " + suffix, "service": "payment-service", "severity": "SEV-3", "impact": "subscriber 628123456789"}, base.Add(time.Minute))
	insert(`INSERT INTO deployment.records(id,service,environment,version,commit_sha,deployer,status,occurred_at)
VALUES($1,'payment-service','development',$2,'abcdef0','search-test','Completed',$3)`, "DEP-"+suffix, "v-"+suffix, base.Add(2*time.Minute))
	insert(`INSERT INTO simulation.runs(id,creation_key,request,environment,percentage,started_at,expires_at,reason)
VALUES($1,$2,'{}','development',5,$3,$4,'subscriber 628123456789')`, "SIM-"+suffix, "sim-search-"+suffix, base.Add(3*time.Minute), base.Add(4*time.Minute))
	insert(`INSERT INTO incident.records(id,creation_key,request_hash,environment,state,version,document,updated_at,detected_at)
VALUES($1,$2,'search-test','staging','Detected',1,$3,$4,$4)`, "INC-staging-"+suffix, "incident-staging-search-"+suffix,
		map[string]any{"id": "INC-staging-" + suffix, "environment": "staging", "state": "Detected", "version": 1,
			"detected_at": base, "title": "Router " + suffix}, base)

	search := func(environment, query string) WorkspaceResults {
		t.Helper()
		rows, err := tx.Query(ctx, workspaceSearchSQL, environment, strings.ToLower(query))
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		result, err := scanWorkspaceHits(rows)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	result := search("development", suffix)
	if len(result.Items) != 4 || result.Items[0].Source != "transaction" {
		t.Fatalf("cross-source search/ranking: %+v", result.Items)
	}
	seen := map[string]bool{}
	for _, hit := range result.Items {
		seen[hit.Source] = true
	}
	if !seen["incident"] || !seen["deployment"] || !seen["simulation"] {
		t.Fatalf("missing search source: %+v", result.Items)
	}
	raw, _ := json.Marshal(result)
	if strings.Contains(string(raw), "628123456789") || strings.Contains(string(raw), "Private customer") {
		t.Fatalf("private source data leaked: %s", raw)
	}
	if staged := search("staging", suffix); len(staged.Items) != 1 || staged.Items[0].Source != "incident" {
		t.Fatalf("environment isolation: %+v", staged.Items)
	}
	if exact := search("development", "INC-"+suffix); len(exact.Items) != 1 || exact.Items[0].ResourceID != "INC-"+suffix {
		t.Fatalf("exact incident ID: %+v", exact.Items)
	}
	if trace := search("development", "trace-"+suffix); len(trace.Items) != 1 || trace.Items[0].ResourceID != suffix {
		t.Fatalf("trace ID lookup: %+v", trace.Items)
	}
	if wildcard := search("development", suffix+"%_"); len(wildcard.Items) != 0 {
		t.Fatalf("search treated wildcard as pattern: %+v", wildcard.Items)
	}
}
