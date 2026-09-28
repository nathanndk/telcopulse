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

func TestActionRegisterFiltersLegacyAndPages(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL required for action register integration")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	store := Store{Pool: pool}
	now := time.Now().UTC().Truncate(time.Second)
	overdue := now.Add(-24 * time.Hour)
	tomorrow := now.Add(24 * time.Hour)
	insert := func(environment, title string, actions []ActionItem) string {
		t.Helper()
		suffix, err := domain.NewID(12)
		if err != nil {
			t.Fatal(err)
		}
		id := "INC-" + suffix
		item := Incident{Fields: Fields{Title: title, Severity: "SEV-2", ActionItems: actions}, ID: id,
			Environment: environment, Service: "payment-service", State: Resolved, Version: 1,
			CreatedAt: now, DetectedAt: now, UpdatedAt: now}
		document, err := json.Marshal(item)
		if err != nil {
			t.Fatal(err)
		}
		_, err = pool.Exec(ctx, `INSERT INTO incident.records(id,creation_key,request_hash,environment,state,version,document,updated_at)
		 VALUES($1,$2,'action-test',$3,$4,1,$5,$6)`, id, "action-"+suffix, environment, Resolved, document, now)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	first := insert("development", "Payment pool exhaustion", []ActionItem{
		{Title: "Increase connection pool", Owner: "platform", Priority: "P1", Status: "Open", DueAt: &overdue},
		{Title: "Document failover", Owner: "network"},
		{Title: "Check logs", Owner: "platform", Done: true, DueAt: &overdue},
	})
	second := insert("development", "Card provider timeout", []ActionItem{
		{Title: "Review provider limits", Owner: "platform", Priority: "P2", Status: "Blocked", DueAt: &tomorrow},
	})
	insert("staging", "Staging incident", []ActionItem{{Title: "Staging only", Owner: "platform", Priority: "P1", Status: "Open"}})
	insert("development", "Legacy incident without actions", nil)

	filter := ActionFilter{Environment: "development", Status: "active", Limit: 25}
	page, err := store.Actions(ctx, filter)
	if err != nil || page.More || len(page.Items) != 3 {
		t.Fatalf("active page: %+v %v", page, err)
	}
	if page.Items[0].IncidentID != first || page.Items[0].Title != "Increase connection pool" || page.Items[0].Priority != "P1" || page.Items[0].Position != 1 || page.Items[0].DueAt == nil {
		t.Fatalf("overdue priority order: %+v", page.Items[0])
	}
	if page.Items[1].IncidentID != second || page.Items[1].Status != "Blocked" || page.Items[2].Title != "Document failover" || page.Items[2].Priority != "P2" || page.Items[2].Status != "Open" {
		t.Fatalf("legacy/undated order: %+v", page.Items)
	}
	filter.Status = "all"
	all, err := store.Actions(ctx, filter)
	if err != nil || len(all.Items) != 4 || all.Items[3].Title != "Check logs" || all.Items[3].Status != "Completed" {
		t.Fatalf("legacy completed item: %+v %v", all, err)
	}
	filter.Status = "active"
	filter.Priority = "P1"
	one, err := store.Actions(ctx, filter)
	if err != nil || len(one.Items) != 1 || one.Items[0].IncidentID != first {
		t.Fatalf("priority filter: %+v %v", one, err)
	}
	filter.Priority = ""
	filter.Owner = "NET"
	owner, err := store.Actions(ctx, filter)
	if err != nil || len(owner.Items) != 1 || owner.Items[0].Title != "Document failover" {
		t.Fatalf("owner filter: %+v %v", owner, err)
	}
	filter.Owner = ""
	filter.Search = "provider"
	search, err := store.Actions(ctx, filter)
	if err != nil || len(search.Items) != 1 || search.Items[0].IncidentID != second {
		t.Fatalf("literal search: %+v %v", search, err)
	}
	filter.Search = ""
	filter.Limit = 2
	firstPage, err := store.Actions(ctx, filter)
	if err != nil || !firstPage.More || firstPage.Next == "" || len(firstPage.Items) != 2 {
		t.Fatalf("first page: %+v %v", firstPage, err)
	}
	filter.Cursor = firstPage.Next
	lastPage, err := store.Actions(ctx, filter)
	if err != nil || lastPage.More || len(lastPage.Items) != 1 || lastPage.Items[0].Title != "Document failover" {
		t.Fatalf("last page: %+v %v", lastPage, err)
	}
	filter.Priority = "P1"
	if _, err := store.Actions(ctx, filter); !errors.Is(err, ErrInvalid) {
		t.Fatal("accepted cursor after filter changed")
	}
	for _, invalidFilter := range []ActionFilter{
		{Environment: "production", Status: "active", Limit: 25},
		{Environment: "development", Status: "other", Limit: 25},
		{Environment: "development", Status: "active", Limit: 101},
	} {
		if _, err := store.Actions(ctx, invalidFilter); !errors.Is(err, ErrInvalid) {
			t.Fatalf("accepted invalid action filter %+v", invalidFilter)
		}
	}
}
