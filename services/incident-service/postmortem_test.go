package incident

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"testing"
	"time"
)

func TestPostmortemNarrativeValidation(t *testing.T) {
	input := GeneratePostmortem{Summary: "Payment recovered", Detection: "Business SLI alert", ContributingFactors: "Pool sizing", WhatWentWell: "Fast triage", WhatWentWrong: "Slow rollback", Note: "Reviewed with team"}
	if err := validatePostmortem(input); err != nil {
		t.Fatal(err)
	}
	input.WhatWentWrong = " "
	if err := validatePostmortem(input); err == nil {
		t.Fatal("empty learning section accepted")
	}
}

func TestMajorPostmortemGenerationIsAtomic(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL required for postmortem database test")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	store := Store{Pool: pool}
	fields := completeFields()
	fields.Impact = "Forty synthetic purchases failed"
	fields.ActionItems = []ActionItem{{Title: "Cap payment concurrency", Owner: "platform", Priority: "P1", Status: "Open"}}
	key := fmt.Sprintf("postmortem-test-%d", time.Now().UnixNano())
	current, _, err := store.Create(ctx, Create{Fields: fields, Environment: "development", Service: "payment-service"}, key, "test-commander")
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []State{Acknowledged, Investigating, Identified, Mitigating, Monitoring, Resolved} {
		change := Update{Fields: fields, State: state, ExpectedVersion: current.Version, Note: "Verified state"}
		if state == Resolved {
			change.RecoveryValidation = recoveryAt(time.Now().UTC())
		}
		current, err = store.UpdateAs(ctx, current.ID, "test-commander", "Incident Commander", change)
		if err != nil {
			t.Fatalf("%s: %v", state, err)
		}
	}
	if current.RecoveryValidation == nil || current.RecoveryValidation.ValidatedBy != "test-commander" || current.RecoveryValidation.ValidatedAt.IsZero() {
		t.Fatalf("recovery actor/time missing: %+v", current.RecoveryValidation)
	}
	if _, err = store.UpdateAs(ctx, current.ID, "test-commander", "Incident Commander", Update{Fields: fields, State: Postmortem, ExpectedVersion: current.Version, Note: "Bypass report"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("major incident bypass accepted: %v", err)
	}
	input := GeneratePostmortem{ExpectedVersion: current.Version, Summary: "Payment purchase degradation", Detection: "Business success rate alert", ContributingFactors: "Undersized connection pool", WhatWentWell: "Trace linked timeout", WhatWentWrong: "Rollback delayed", Note: "Retrospective approved"}
	if _, err = store.GeneratePostmortemAs(ctx, current.ID, "test-operator", "Operator", input); !errors.Is(err, ErrForbidden) {
		t.Fatalf("operator generated report: %v", err)
	}
	report, err := store.GeneratePostmortemAs(ctx, current.ID, "test-commander", "Incident Commander", input)
	if err != nil {
		t.Fatal(err)
	}
	if report.IncidentVersion != current.Version+1 || report.Impact != fields.Impact || report.RootCause != fields.RootCause || report.Mitigation != fields.Mitigation || report.ActionItems[0].Priority != "P1" || len(report.Timeline) != 8 || report.Timeline[7].Action != "postmortem_generated" {
		t.Fatalf("incomplete report snapshot: %+v", report)
	}
	detail, err := store.Get(ctx, current.ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Incident.State != Postmortem || detail.Postmortem == nil || detail.Postmortem.Summary != input.Summary || detail.History[7].Action != "postmortem_generated" {
		t.Fatalf("report/incident/audit did not commit together: %+v", detail)
	}
	if _, err = store.GeneratePostmortemAs(ctx, current.ID, "test-commander", "Incident Commander", input); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale generation accepted: %v", err)
	}
	input.ExpectedVersion = detail.Incident.Version
	if _, err = store.GeneratePostmortemAs(ctx, current.ID, "test-commander", "Incident Commander", input); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate generation accepted: %v", err)
	}
	if _, err = pool.Exec(ctx, `UPDATE incident.postmortems SET generated_at=now() WHERE incident_id=$1`, current.ID); err == nil {
		t.Fatal("postmortem document was mutable")
	}
}
