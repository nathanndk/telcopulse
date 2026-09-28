package incident

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"telcopulse/services/shared/domain"
)

func TestSavedViewValidation(t *testing.T) {
	valid := SavedViewInput{Environment: "development", Name: "  My triage  ", Filters: SavedViewFilters{State: "Investigating", Severity: "SEV-2", Service: " payment-service ", Range: "24h"}}
	if err := validateSavedView(&valid); err != nil || valid.Name != "My triage" || valid.Filters.Service != "payment-service" || valid.Filters.Sort != "updated_desc" || len(valid.Filters.Columns) != 9 {
		t.Fatalf("normalization: %+v %v", valid, err)
	}
	cases := []SavedViewInput{
		{Environment: "production", Name: "bad", Filters: SavedViewFilters{Range: "all"}},
		{Environment: "development", Name: " ", Filters: SavedViewFilters{Range: "all"}},
		{Environment: "development", Name: "bad", Filters: SavedViewFilters{Range: "yesterday"}},
		{Environment: "development", Name: "bad", Filters: SavedViewFilters{State: "Unknown", Range: "all"}},
		{Environment: "development", Name: "bad", Filters: SavedViewFilters{Range: "all", Sort: "unsafe"}},
		{Environment: "development", Name: "bad", Filters: SavedViewFilters{Range: "all", Columns: []string{"owner"}}},
		{Environment: "development", Name: "bad", Filters: SavedViewFilters{Range: "all", Columns: []string{"incident", "incident"}}},
	}
	for _, input := range cases {
		if err := validateSavedView(&input); !errors.Is(err, ErrInvalid) {
			t.Fatalf("accepted %+v", input)
		}
	}
}

func TestSavedViewOwnershipAndDurability(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL required for saved view persistence")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	store := Store{Pool: pool}
	ownerID, err := domain.NewID(12)
	if err != nil {
		t.Fatal(err)
	}
	otherID, err := domain.NewID(12)
	if err != nil {
		t.Fatal(err)
	}
	owner := "operator:USR-" + ownerID
	other := "operator:USR-" + otherID
	input := SavedViewInput{Environment: "development", Name: "My triage", Filters: SavedViewFilters{State: "Investigating", Range: "24h"}}
	created, err := store.CreateSavedView(ctx, owner, input)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM incident.saved_views WHERE id=$1`, created.ID) })
	own, err := store.ListSavedViews(ctx, owner, "development")
	if err != nil || len(own) == 0 || own[0].ID != created.ID {
		t.Fatalf("own view: %+v %v", own, err)
	}
	hidden, err := store.ListSavedViews(ctx, other, "development")
	if err != nil {
		t.Fatal(err)
	}
	for _, view := range hidden {
		if view.ID == created.ID {
			t.Fatal("view leaked to other operator")
		}
	}
	if _, err = store.UpdateSavedView(ctx, other, created.ID, input); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other operator updated view: %v", err)
	}
	if err = store.DeleteSavedView(ctx, other, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other operator deleted view: %v", err)
	}
	input.Name = "Updated triage"
	input.Filters.Range = "7d"
	updated, err := store.UpdateSavedView(ctx, owner, created.ID, input)
	if err != nil || updated.Filters.Range != "7d" {
		t.Fatalf("update: %+v %v", updated, err)
	}
	if err = store.DeleteSavedView(ctx, owner, created.ID); err != nil {
		t.Fatal(err)
	}
}
