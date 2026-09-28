package incident

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"telcopulse/services/shared/domain"
)

func TestPostmortemIndexScopesFrozenNarrativeAndPages(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL required for postmortem index integration")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	store := Store{Pool: pool}
	empty, err := store.Postmortems(ctx, ReportFilter{Environment: "staging", Limit: 20})
	if err != nil || len(empty.Items) != 0 || empty.More {
		t.Fatalf("empty index: %+v %v", empty, err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	insert := func(environment, service, title, severity string, at time.Time, report PostmortemReport) string {
		t.Helper()
		suffix, err := domain.NewID(12)
		if err != nil {
			t.Fatal(err)
		}
		id := "INC-" + suffix
		item := Incident{Fields: Fields{Title: title, Severity: severity, Mitigation: "Later editable mitigation"}, ID: id,
			Environment: environment, Service: service, State: Postmortem, Version: 8,
			CreatedAt: at, DetectedAt: at, UpdatedAt: at}
		document, err := json.Marshal(item)
		if err != nil {
			t.Fatal(err)
		}
		_, err = pool.Exec(ctx, `INSERT INTO incident.records(id,creation_key,request_hash,environment,state,version,document,updated_at)
		 VALUES($1,$2,'report-index-test',$3,$4,8,$5,$6)`, id, "report-"+suffix, environment, Postmortem, document, at)
		if err != nil {
			t.Fatal(err)
		}
		report.IncidentID = id
		report.IncidentVersion = 8
		report.GeneratedAt = at
		report.GeneratedBy = "test-commander"
		frozen, err := json.Marshal(report)
		if err != nil {
			t.Fatal(err)
		}
		_, err = pool.Exec(ctx, `INSERT INTO incident.postmortems(incident_id,incident_version,document,generated_at)
		 VALUES($1,8,$2,$3)`, id, frozen, at)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	recent := insert("development", "payment-service", "Payment connection exhaustion", "SEV-1", now,
		PostmortemReport{Summary: "Purchases timed out", RootCause: "Connection pool exhaustion", ContributingFactors: "Traffic spike", Mitigation: "Capped concurrency", Resolution: "Capacity restored", WhatWentWell: "Trace correlation", WhatWentWrong: "Slow alert triage"})
	legacy := insert("development", "package-service", "Package cache failure", "SEV-2", now.Add(-time.Hour),
		PostmortemReport{Summary: "Package browsing slowed", RootCause: "Expired cache", ContributingFactors: "Missing fallback", Resolution: "Cache restored", WhatWentWrong: "No fallback drill"})
	insert("staging", "payment-service", "Staging payment issue", "SEV-1", now.Add(-2*time.Hour),
		PostmortemReport{Summary: "Staging only", RootCause: "Test fault", Mitigation: "Stopped test"})

	filter := ReportFilter{Environment: "development", Limit: 20}
	page, err := store.Postmortems(ctx, filter)
	if err != nil || len(page.Items) != 2 || page.More || page.Items[0].IncidentID != recent || page.Items[1].IncidentID != legacy {
		t.Fatalf("environment/order: %+v %v", page, err)
	}
	if page.Items[0].Mitigation != "Capped concurrency" || page.Items[1].Mitigation != "" || page.Items[0].IncidentVersion != 8 {
		t.Fatalf("report narrative was not frozen: %+v", page.Items)
	}
	filter.Service = "payment-service"
	service, err := store.Postmortems(ctx, filter)
	if err != nil || len(service.Items) != 1 || service.Items[0].IncidentID != recent {
		t.Fatalf("service filter: %+v %v", service, err)
	}
	filter.Service = ""
	filter.Severity = "SEV-2"
	severity, err := store.Postmortems(ctx, filter)
	if err != nil || len(severity.Items) != 1 || severity.Items[0].IncidentID != legacy {
		t.Fatalf("severity filter: %+v %v", severity, err)
	}
	filter.Severity = ""
	filter.Search = "FALLBACK"
	search, err := store.Postmortems(ctx, filter)
	if err != nil || len(search.Items) != 1 || search.Items[0].IncidentID != legacy {
		t.Fatalf("narrative search: %+v %v", search, err)
	}
	filter.Search = ""
	filter.Search = "TRACE CORRELATION"
	learning, err := store.Postmortems(ctx, filter)
	if err != nil || len(learning.Items) != 1 || learning.Items[0].IncidentID != recent || learning.Items[0].WhatWentWell != "Trace correlation" {
		t.Fatalf("learning search: %+v %v", learning, err)
	}
	filter.Search = ""
	filter.Limit = 1
	first, err := store.Postmortems(ctx, filter)
	if err != nil || !first.More || first.Next == "" || len(first.Items) != 1 || first.Items[0].IncidentID != recent {
		t.Fatalf("first page: %+v %v", first, err)
	}
	filter.Cursor = first.Next
	second, err := store.Postmortems(ctx, filter)
	if err != nil || second.More || len(second.Items) != 1 || second.Items[0].IncidentID != legacy {
		t.Fatalf("second page: %+v %v", second, err)
	}
	filter.Service = "payment-service"
	if _, err := store.Postmortems(ctx, filter); !errors.Is(err, ErrInvalid) {
		t.Fatal("accepted report cursor after changing service filter")
	}
	for _, bad := range []ReportFilter{
		{Environment: "production", Limit: 20},
		{Environment: "development", Severity: "SEV-0", Limit: 20},
		{Environment: "development", Limit: 101},
	} {
		if _, err := store.Postmortems(ctx, bad); !errors.Is(err, ErrInvalid) {
			t.Fatalf("accepted invalid filter %+v", bad)
		}
	}
}
