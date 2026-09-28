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

func TestIncidentListFiltersAndCursorScope(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL required for incident list integration")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	store := Store{Pool: pool}
	marker, err := domain.NewID(12)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	insert := func(environment, title, service, severity, owner string, detected, updated time.Time) string {
		t.Helper()
		suffix, err := domain.NewID(12)
		if err != nil {
			t.Fatal(err)
		}
		id := "INC-" + suffix
		item := Incident{Fields: Fields{Title: title, Severity: severity, Owner: owner}, ID: id,
			Environment: environment, Service: service, State: Investigating, Version: 1,
			CreatedAt: detected, DetectedAt: detected, UpdatedAt: updated}
		document, err := json.Marshal(item)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO incident.records(id,creation_key,request_hash,environment,state,version,document,updated_at)
			VALUES($1,$2,'list-filter-test',$3,$4,1,$5,$6)`, id, "list-"+suffix, environment, Investigating, document, updated); err != nil {
			t.Fatal(err)
		}
		return id
	}
	recent := insert("development", marker+" payment timeout", "payment-service", "SEV-1", "Alice Commander", now, now)
	older := insert("development", marker+" package delay", "package-service", "SEV-2", "Bob Operator", now.Add(-48*time.Hour), now.Add(-time.Hour))
	insert("staging", marker+" staging payment", "payment-service", "SEV-1", "Alice Commander", now, now.Add(time.Minute))
	filter := ListFilter{Environment: "development", State: Investigating, Search: marker, Limit: 20}
	page, err := store.ListFiltered(ctx, filter)
	if err != nil || len(page.Items) != 2 || page.More || page.Items[0].ID != recent || page.Items[1].ID != older {
		t.Fatalf("environment, title and update order: %+v %v", page, err)
	}
	filter.Severity = "SEV-1"
	filter.Service = "payment-service"
	filter.Owner = "ALICE"
	filtered, err := store.ListFiltered(ctx, filter)
	if err != nil || len(filtered.Items) != 1 || filtered.Items[0].ID != recent {
		t.Fatalf("severity, service and owner filters: %+v %v", filtered, err)
	}
	filter.Since = now.Add(-24 * time.Hour).Format(time.RFC3339)
	filtered, err = store.ListFiltered(ctx, filter)
	if err != nil || len(filtered.Items) != 1 || filtered.Items[0].ID != recent {
		t.Fatalf("detected-time filter: %+v %v", filtered, err)
	}
	filter.Service, filter.Owner, filter.Severity, filter.Since = "", "", "", ""
	filter.Search = recent
	byID, err := store.ListFiltered(ctx, filter)
	if err != nil || len(byID.Items) != 1 || byID.Items[0].ID != recent {
		t.Fatalf("ID search: %+v %v", byID, err)
	}
	filter.Search = marker
	filter.Limit = 1
	first, err := store.ListFiltered(ctx, filter)
	if err != nil || len(first.Items) != 1 || !first.More || first.Next == "" || first.Items[0].ID != recent {
		t.Fatalf("first page: %+v %v", first, err)
	}
	filter.Cursor = first.Next
	second, err := store.ListFiltered(ctx, filter)
	if err != nil || len(second.Items) != 1 || second.More || second.Items[0].ID != older {
		t.Fatalf("second page: %+v %v", second, err)
	}
	filter.Severity = "SEV-1"
	if _, err := store.ListFiltered(ctx, filter); !errors.Is(err, ErrInvalid) {
		t.Fatal("accepted a cursor with changed filters")
	}
	filter.Severity, filter.Cursor, filter.Sort = "", "", "detected_asc"
	ascending, err := store.ListFiltered(ctx, filter)
	if err != nil || len(ascending.Items) != 1 || ascending.Items[0].ID != older || !ascending.More {
		t.Fatalf("oldest opened first: %+v %v", ascending, err)
	}
	filter.Cursor = ascending.Next
	ascendingSecond, err := store.ListFiltered(ctx, filter)
	if err != nil || len(ascendingSecond.Items) != 1 || ascendingSecond.Items[0].ID != recent || ascendingSecond.More {
		t.Fatalf("oldest opened second page: %+v %v", ascendingSecond, err)
	}
	filter.Sort = "detected_desc"
	if _, err := store.ListFiltered(ctx, filter); !errors.Is(err, ErrInvalid) {
		t.Fatal("accepted a cursor with changed sort")
	}
	filter.Cursor, filter.Sort = "", "severity_asc"
	priority, err := store.ListFiltered(ctx, filter)
	if err != nil || len(priority.Items) != 1 || priority.Items[0].ID != recent || !priority.More {
		t.Fatalf("highest severity first: %+v %v", priority, err)
	}
	filter.Cursor = priority.Next
	prioritySecond, err := store.ListFiltered(ctx, filter)
	if err != nil || len(prioritySecond.Items) != 1 || prioritySecond.Items[0].ID != older {
		t.Fatalf("severity cursor: %+v %v", prioritySecond, err)
	}
	for _, bad := range []ListFilter{
		{Environment: "production", Limit: 20},
		{Environment: "development", Severity: "SEV-0", Limit: 20},
		{Environment: "development", Since: "yesterday", Limit: 20},
		{Environment: "development", Search: string(make([]byte, 101)), Limit: 20},
		{Environment: "development", Sort: "severity_desc;DROP TABLE incident.records", Limit: 20},
	} {
		if _, err := store.ListFiltered(ctx, bad); !errors.Is(err, ErrInvalid) {
			t.Fatalf("accepted invalid filters: %+v", bad)
		}
	}
}
