package workflow

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"telcopulse/services/api-gateway/internal/store"
)

func TestWorkflowSlotsUseHalfThePool(t *testing.T) {
	// pgxpool connects lazily; no database is needed to size the slots.
	pool, err := pgxpool.New(context.Background(), "postgres://test@127.0.0.1:1/test?pool_max_conns=4")
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	s := &Service{Store: &store.Store{Pool: pool}}
	first, ok1 := s.acquireSlot(context.Background())
	_, ok2 := s.acquireSlot(context.Background())
	if !ok1 || !ok2 || s.InFlight() != 2 {
		t.Fatalf("two slots expected for a four-connection pool, in flight %d", s.InFlight())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, ok := s.acquireSlot(ctx); ok || s.Deferred() != 1 {
		t.Fatalf("third workflow admitted beyond half the pool (deferred %d)", s.Deferred())
	}
	first()
	if release, ok := s.acquireSlot(context.Background()); !ok {
		t.Fatal("released slot was not reusable")
	} else {
		release()
	}
}
