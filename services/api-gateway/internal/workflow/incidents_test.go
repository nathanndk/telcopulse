package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"sync"
	incident "telcopulse/services/incident-service"
	"telcopulse/services/shared/domain"
	rt "telcopulse/services/shared/runtime"
	"testing"
	"time"
)

func TestIncidentAtomicRevisions(t *testing.T) {
	service := setup(t, nil, nil, nil)
	repo := incident.Store{Pool: service.Pool}
	ctx := context.Background()
	key, err := domain.NewID(16)
	if err != nil {
		t.Fatal(err)
	}
	input := incident.Create{Fields: incident.Fields{Title: "Payment failures elevated", Severity: "SEV-2"}, Environment: "development", Service: "payment-service"}
	first, replay, err := repo.Create(ctx, input, key, "local-operator")
	if err != nil || replay {
		t.Fatalf("create: %v", err)
	}
	second, replay, err := repo.Create(ctx, input, key, "local-operator")
	if err != nil || !replay || second.ID != first.ID {
		t.Fatalf("replay: %v", err)
	}
	input.Title = "Different request"
	if _, _, err = repo.Create(ctx, input, key, "local-operator"); !errors.Is(err, incident.ErrConflict) {
		t.Fatalf("missing idempotency conflict: %v", err)
	}
	change := incident.Update{Fields: first.Fields, State: incident.Acknowledged, ExpectedVersion: 1, Note: "On-call accepted"}
	change.Owner = "operator-1"
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); _, e := repo.Update(ctx, first.ID, "local-operator", change); results <- e }()
	}
	wg.Wait()
	close(results)
	successes, conflicts := 0, 0
	for e := range results {
		if e == nil {
			successes++
		} else if errors.Is(e, incident.ErrConflict) {
			conflicts++
		} else {
			t.Fatal(e)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("concurrency: success=%d conflict=%d", successes, conflicts)
	}
	detail, err := repo.Get(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Incident.Version != 2 || len(detail.History) != 2 || detail.History[1].Before.State != incident.Detected || detail.History[1].After.State != incident.Acknowledged {
		t.Fatal("audit does not match mutation")
	}
	if _, err = service.Pool.Exec(ctx, "UPDATE incident.audit SET entry='{}' WHERE incident_id=$1", first.ID); err == nil {
		t.Fatal("audit update allowed")
	}
	if _, err = service.Pool.Exec(ctx, "DELETE FROM incident.audit WHERE incident_id=$1", first.ID); err == nil {
		t.Fatal("audit deletion allowed")
	}
	// A failed audit insertion must roll back the accompanying incident update.
	if _, err = service.Pool.Exec(ctx, `CREATE FUNCTION incident.reject_test_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'forced audit failure'; END $$; CREATE TRIGGER reject_test_audit BEFORE INSERT ON incident.audit FOR EACH ROW EXECUTE FUNCTION incident.reject_test_audit()`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, e := service.Pool.Exec(context.Background(), `DROP TRIGGER reject_test_audit ON incident.audit; DROP FUNCTION incident.reject_test_audit()`); e != nil {
			t.Error(e)
		}
	})
	change.ExpectedVersion = 2
	change.State = incident.Investigating
	if _, err = repo.Update(ctx, first.ID, "local-operator", change); err == nil {
		t.Fatal("audit failure ignored")
	}
	detail, err = repo.Get(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Incident.Version != 2 || len(detail.History) != 2 {
		t.Fatal("partial incident update committed")
	}
}

func TestOperatorIncidentPermissionsAreAtomic(t *testing.T) {
	service := setup(t, nil, nil, nil)
	repo := incident.Store{Pool: service.Pool}
	ctx := context.Background()
	key, err := domain.NewID(16)
	if err != nil {
		t.Fatal(err)
	}
	created, _, err := repo.Create(ctx, incident.Create{Fields: incident.Fields{Title: "Operator permission test", Severity: "SEV-3", Evidence: []incident.Evidence{{Kind: "metric", Summary: "Initial"}}}, Environment: "development", Service: "payment-service"}, key, "local-test")
	if err != nil {
		t.Fatal(err)
	}
	change := incident.Update{Fields: created.Fields, State: incident.Acknowledged, ExpectedVersion: created.Version, Note: "On-call accepted the incident"}
	change.Owner = "on-call"
	change.Evidence = append(change.Evidence, incident.Evidence{Kind: "trace", Summary: "Payment timeout"})
	updated, err := repo.UpdateAs(ctx, created.ID, "operator:USR-0123456789abcdef01234567", "Operator", change)
	if err != nil || updated.State != incident.Acknowledged || updated.Owner != "on-call" || len(updated.Evidence) != 2 {
		t.Fatalf("operator acknowledgement failed: %+v %v", updated, err)
	}
	change.ExpectedVersion = updated.Version
	change.State = incident.Investigating
	change.Severity = "SEV-1"
	if _, err = repo.UpdateAs(ctx, created.ID, "operator:USR-0123456789abcdef01234567", "Operator", change); !errors.Is(err, incident.ErrForbidden) {
		t.Fatalf("operator severity change accepted: %v", err)
	}
	detail, err := repo.Get(ctx, created.ID)
	if err != nil || detail.Incident.Version != updated.Version || detail.Incident.Severity != "SEV-3" || len(detail.History) != 2 || detail.History[1].Actor != "operator:USR-0123456789abcdef01234567" {
		t.Fatalf("forbidden edit changed incident or audit: %+v %v", detail, err)
	}
	change.Severity = "SEV-1"
	commander, err := repo.UpdateAs(ctx, created.ID, "operator:USR-ffffffffffffffffffffffff", "Incident Commander", change)
	if err != nil || commander.Severity != "SEV-1" {
		t.Fatalf("commander severity change denied: %+v %v", commander, err)
	}
}

func TestCommanderEscalationIsAtomicAndPersistsThroughEdits(t *testing.T) {
	service := setup(t, nil, nil, nil)
	repo := incident.Store{Pool: service.Pool}
	ctx := context.Background()
	key, err := domain.NewID(16)
	if err != nil {
		t.Fatal(err)
	}
	created, _, err := repo.Create(ctx, incident.Create{Fields: incident.Fields{Title: "Escalation coordination", Severity: "SEV-3"}, Environment: "development", Service: "payment-service"}, key, "local-test")
	if err != nil {
		t.Fatal(err)
	}
	command := incident.Escalate{ExpectedVersion: created.Version, Team: "Payments L2", Owner: "payments-on-call", Severity: "SEV-2", Reason: "Cross-service payment failures require L2 coordination"}
	if _, err = repo.EscalateAs(ctx, created.ID, "operator:USR-0123456789abcdef01234567", "Operator", command); !errors.Is(err, incident.ErrForbidden) {
		t.Fatalf("Operator escalation accepted: %v", err)
	}
	escalated, err := repo.EscalateAs(ctx, created.ID, "operator:USR-ffffffffffffffffffffffff", "Incident Commander", command)
	if err != nil || escalated.Version != 2 || escalated.EscalationLevel != 1 || escalated.OwningTeam != "Payments L2" || escalated.Owner != "payments-on-call" || escalated.Severity != "SEV-2" || escalated.EscalatedAt == nil {
		t.Fatalf("Commander escalation: %+v %v", escalated, err)
	}
	if _, err = repo.EscalateAs(ctx, created.ID, "operator:USR-ffffffffffffffffffffffff", "Incident Commander", command); !errors.Is(err, incident.ErrConflict) {
		t.Fatalf("stale escalation accepted: %v", err)
	}
	command.ExpectedVersion = escalated.Version
	command.Severity = "SEV-4"
	if _, err = repo.EscalateAs(ctx, created.ID, "operator:USR-ffffffffffffffffffffffff", "Incident Commander", command); !errors.Is(err, incident.ErrInvalid) {
		t.Fatalf("severity downgrade accepted: %v", err)
	}
	changed, err := repo.UpdateAs(ctx, created.ID, "operator:USR-0123456789abcdef01234567", "Operator", incident.Update{Fields: escalated.Fields, State: incident.Acknowledged, ExpectedVersion: escalated.Version, Note: "Acknowledge L2 handoff"})
	if err != nil || changed.OwningTeam != "Payments L2" || changed.EscalationLevel != 1 {
		t.Fatalf("ordinary edit lost escalation: %+v %v", changed, err)
	}
	detail, err := repo.Get(ctx, created.ID)
	if err != nil || len(detail.History) != 3 || detail.History[1].Action != "escalated" || detail.History[1].Before.EscalationLevel != 0 || detail.History[1].After.EscalationLevel != 1 || detail.History[1].Actor != "operator:USR-ffffffffffffffffffffffff" {
		t.Fatalf("escalation audit changed: %+v %v", detail, err)
	}
}

func TestIncidentInternalAPI(t *testing.T) {
	service := setup(t, nil, nil, nil)
	log := slog.New(slog.NewJSONHandler(io.Discard, nil))
	mutationToken := "incident-mutation-test-token-123456"
	handler := rt.Protect(incident.Handler(service.Pool, log, mutationToken), "incident-test-token")
	key, err := domain.NewID(16)
	if err != nil {
		t.Fatal(err)
	}
	call := func(method, path, body, token string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set(rt.MutationTokenHeader, mutationToken)
		r.Header.Set("Idempotency-Key", key)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	if w := call("POST", "/internal/incidents", `{"title":"`+strings.Repeat("x", 128<<10)+`"}`, "incident-test-token"); w.Code != 400 {
		t.Fatal("oversized body accepted")
	}
	input := incident.Create{Fields: incident.Fields{Title: "Internal API incident", Severity: "SEV-3", Impact: strings.Repeat("x", 3500), RootCause: strings.Repeat("y", 3500), Mitigation: strings.Repeat("z", 3500)}, Environment: "staging", Service: "payment-service"}
	body, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	if w := call("POST", "/internal/incidents", string(body), "wrong"); w.Code != 401 {
		t.Fatal("missing authentication")
	}
	missingMutationToken := httptest.NewRequest("POST", "/internal/incidents", strings.NewReader(string(body)))
	missingMutationToken.Header.Set("Authorization", "Bearer incident-test-token")
	missingMutationToken.Header.Set("Idempotency-Key", key)
	missingResult := httptest.NewRecorder()
	handler.ServeHTTP(missingResult, missingMutationToken)
	if missingResult.Code != 401 {
		t.Fatalf("shared token created an incident without mutation capability: %d", missingResult.Code)
	}
	w := call("POST", "/internal/incidents", string(body), "incident-test-token")
	if w.Code != 201 {
		t.Fatalf("create %d: %s", w.Code, w.Body)
	}
	var created incident.Incident
	if err = json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if w = call("POST", "/internal/incidents", string(body), "incident-test-token"); w.Code != 200 {
		t.Fatal("replay did not return 200")
	}
	viewer := httptest.NewRequest("POST", "/internal/incidents", strings.NewReader(string(body)))
	viewer.Header.Set("Authorization", "Bearer incident-test-token")
	viewer.Header.Set(rt.MutationTokenHeader, mutationToken)
	viewer.Header.Set("Idempotency-Key", key)
	viewer.Header.Set("X-Operator-Actor", "operator:USR-0123456789abcdef01234567")
	viewer.Header.Set("X-Operator-Role", "Viewer")
	viewerResult := httptest.NewRecorder()
	handler.ServeHTTP(viewerResult, viewer)
	if viewerResult.Code != 403 {
		t.Fatalf("viewer creation accepted: %d", viewerResult.Code)
	}
	if w = call("GET", "/internal/incidents/"+created.ID, "", "incident-test-token"); w.Code != 200 {
		t.Fatal("detail unavailable")
	}
	if w = call("GET", "/internal/incidents?environment=staging&state=Detected&limit=100", "", "incident-test-token"); w.Code != 200 || !strings.Contains(w.Body.String(), created.ID) {
		t.Fatal("filtered list missing incident")
	}
	if w = call("PUT", "/internal/incidents/"+created.ID, `{"actor":"admin"}`, "incident-test-token"); w.Code != 400 {
		t.Fatal("client-supplied actor accepted")
	}
}

func TestIncidentPagination(t *testing.T) {
	service := setup(t, nil, nil, nil)
	repo := incident.Store{Pool: service.Pool}
	ctx := context.Background()
	key, err := domain.NewID(16)
	if err != nil {
		t.Fatal(err)
	}
	current, _, err := repo.Create(ctx, incident.Create{Fields: incident.Fields{Title: "Pagination verification", Severity: "SEV-4"}, Environment: "staging", Service: "payment-service"}, key, "local-test")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 11; i++ {
		current, err = repo.Update(ctx, current.ID, "local-test", incident.Update{Fields: current.Fields, State: current.State, ExpectedVersion: current.Version, Note: "Revision for pagination"})
		if err != nil {
			t.Fatal(err)
		}
	}
	first, err := repo.Get(ctx, current.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.History) != 10 || !first.HistoryMore || first.HistoryNext != 10 || first.Incident.Version != 12 {
		t.Fatalf("invalid first page: %+v", first)
	}
	current, err = repo.Update(ctx, current.ID, "local-test", incident.Update{Fields: current.Fields, State: current.State, ExpectedVersion: current.Version, Note: "Concurrent append"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := repo.GetHistory(ctx, current.ID, first.HistoryNext)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.History) != 3 || second.HistoryMore || second.History[0].Version != 11 || second.History[2].Version != 13 || second.Incident.Version != 13 {
		t.Fatal("history cursor skipped or repeated revisions")
	}
	empty, err := repo.GetHistory(ctx, current.ID, 13)
	if err != nil || len(empty.History) != 0 || empty.HistoryMore {
		t.Fatal("invalid terminal page")
	}
	if _, err = repo.GetHistory(ctx, current.ID, -1); !errors.Is(err, incident.ErrInvalid) {
		t.Fatal("negative history cursor accepted")
	}
	page, err := repo.ListAfter(ctx, "staging", "", 1, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].ID != current.ID {
		t.Fatal("latest record missing")
	}
	// Create a second record and give both the same database timestamp to verify ID tie-breaking.
	key, err = domain.NewID(16)
	if err != nil {
		t.Fatal(err)
	}
	other, _, err := repo.Create(ctx, incident.Create{Fields: current.Fields, Environment: "staging", Service: "payment-service"}, key, "local-test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.Pool.Exec(ctx, `UPDATE incident.records SET updated_at='2099-01-01T00:00:00Z' WHERE id=ANY($1)`, []string{current.ID, other.ID}); err != nil {
		t.Fatal(err)
	}
	page, err = repo.ListAfter(ctx, "staging", "", 1, "")
	if err != nil || !page.More || page.Next == "" {
		t.Fatalf("missing cursor: %v", err)
	}
	next, err := repo.ListAfter(ctx, "staging", "", 1, page.Next)
	if err != nil || len(next.Items) != 1 || next.Items[0].ID == page.Items[0].ID {
		t.Fatalf("tie boundary: %v", err)
	}
	ids := map[string]bool{page.Items[0].ID: true, next.Items[0].ID: true}
	if !ids[current.ID] || !ids[other.ID] {
		t.Fatal("tie skipped record")
	}
	for _, cursor := range []string{"malformed", page.Next} {
		env := "staging"
		if cursor == page.Next {
			env = "development"
		}
		if _, err = repo.ListAfter(ctx, env, "", 1, cursor); !errors.Is(err, incident.ErrInvalid) {
			t.Fatal("invalid cursor accepted")
		}
	}
	// Restore the synthetic ordering so this fixture does not dominate future lists.
	if _, err = service.Pool.Exec(ctx, `UPDATE incident.records SET updated_at=(document->>'updated_at')::timestamptz WHERE id=ANY($1)`, []string{current.ID, other.ID}); err != nil {
		t.Fatal(err)
	}
}

func TestAlertEpisodeDeduplication(t *testing.T) {
	service := setup(t, nil, nil, nil)
	repo := incident.Store{Pool: service.Pool}
	ctx := context.Background()
	observation := incident.AlertObservation{State: "firing", ActiveAt: time.Now().UTC().Add(-time.Minute), Labels: map[string]string{"alertname": "ServiceUnavailable", "service": "payment-service", "instance": "payment-service:8080"}}
	first, replay, err := repo.IngestAlert(ctx, "test-prometheus", "development", "http://localhost:9090", observation)
	if err != nil || replay {
		t.Fatalf("first observation: %v", err)
	}
	edited, err := repo.Update(ctx, first.ID, "operator", incident.Update{Fields: first.Fields, State: incident.Acknowledged, ExpectedVersion: 1, Note: "Ownership assigned"})
	// An owner is required, so this incomplete edit must fail.
	if err == nil {
		t.Fatal("unowned acknowledgement allowed")
	}
	fields := first.Fields
	fields.Owner = "on-call"
	fields.Title = "Operator investigation"
	edited, err = repo.Update(ctx, first.ID, "operator", incident.Update{Fields: fields, State: incident.Acknowledged, ExpectedVersion: 1, Note: "Ownership assigned"})
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		again, replay, err := repo.IngestAlert(ctx, "test-prometheus", "development", "http://changed-viewer:9090", observation)
		if err != nil || !replay || again.ID != first.ID || again.Title != edited.Title || again.Version != 2 {
			t.Fatalf("replay changed operator data: %v", err)
		}
	}
	detail, err := repo.Get(ctx, first.ID)
	if err != nil || len(detail.History) != 2 || detail.History[0].Actor != "prometheus-alert" {
		t.Fatal("replay appended history or lost source actor")
	}
	observation.ActiveAt = observation.ActiveAt.Add(time.Second)
	next, replay, err := repo.IngestAlert(ctx, "test-prometheus", "development", "", observation)
	if err != nil || replay || next.ID == first.ID {
		t.Fatalf("new episode not created: %v", err)
	}
}

func TestIncidentOverviewScopeAndLifecycle(t *testing.T) {
	service := setup(t, nil, nil, nil)
	repo := incident.Store{Pool: service.Pool}
	ctx := context.Background()
	baseline, err := repo.Overview(ctx, "staging")
	if err != nil {
		t.Fatal(err)
	}
	create := func(env, severity string) incident.Incident {
		t.Helper()
		key, err := domain.NewID(16)
		if err != nil {
			t.Fatal(err)
		}
		item, _, err := repo.Create(ctx, incident.Create{Fields: incident.Fields{Title: "Overview verification", Severity: severity, Owner: "on-call", RootCause: "Synthetic", Mitigation: "Synthetic", Resolution: "Synthetic"}, Environment: env, Service: "payment-service"}, key, "test")
		if err != nil {
			t.Fatal(err)
		}
		return item
	}
	high := create("staging", "SEV-1")
	low := create("staging", "SEV-4")
	create("development", "SEV-1")
	page, err := repo.Overview(ctx, "staging")
	if err != nil {
		t.Fatal(err)
	}
	if page.Active != baseline.Active+2 || page.Critical != baseline.Critical+1 || len(page.Items) > 5 || page.Items[0].ID != high.ID {
		t.Fatalf("incorrect overview: %+v", page)
	}
	for _, state := range []incident.State{incident.Acknowledged, incident.Investigating, incident.Identified, incident.Mitigating, incident.Monitoring, incident.Resolved} {
		high, err = repo.Update(ctx, high.ID, "test", incident.Update{Fields: high.Fields, ExpectedVersion: high.Version, State: state, Note: "Overview lifecycle test"})
		if err != nil {
			t.Fatal(err)
		}
	}
	page, err = repo.Overview(ctx, "staging")
	if err != nil || page.Active != baseline.Active+1 || page.Critical != baseline.Critical {
		t.Fatalf("resolved incident counted: %+v %v", page, err)
	}
	for _, i := range page.Items {
		if i.ID == high.ID {
			t.Fatal("resolved incident in active summaries")
		}
	}
	if low.ID == high.ID {
		t.Fatal("unexpected identity collision")
	}
	if _, err = repo.Overview(ctx, "unknown"); !errors.Is(err, incident.ErrInvalid) {
		t.Fatal("invalid environment accepted")
	}
}
