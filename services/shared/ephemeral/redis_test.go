package ephemeral

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"telcopulse/services/shared/domain"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	url := os.Getenv("TEST_REDIS_URL")
	if url == "" {
		t.Skip("TEST_REDIS_URL required")
	}
	s, err := Open(url, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	s.Prefix = fmt.Sprintf("telcopulse-test:%d:", time.Now().UnixNano())
	t.Cleanup(func() {
		if err := s.Client.Close(); err != nil {
			t.Error(err)
		}
	})
	return s
}
func TestSharedBudget(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	var admitted atomic.Int32
	var wg sync.WaitGroup
	for range 24 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			retry, err := s.Allow(ctx, "test", 5, time.Second)
			if err != nil {
				t.Error(err)
			} else if retry == 0 {
				admitted.Add(1)
			}
		}()
	}
	wg.Wait()
	if admitted.Load() != 5 {
		t.Fatalf("admitted %d", admitted.Load())
	}
	// A distinct client models another gateway instance sharing the same budget.
	second := testStore(t)
	second.Prefix = s.Prefix
	retry, err := second.Allow(ctx, "test", 5, time.Second)
	if err != nil || retry <= 0 {
		t.Fatalf("replica bypassed limit: %v %v", retry, err)
	}
	time.Sleep(time.Second + 20*time.Millisecond)
	retry, err = s.Allow(ctx, "test", 5, time.Second)
	if err != nil || retry != 0 {
		t.Fatalf("window did not expire %v %v", retry, err)
	}
}
func TestCatalogCacheAndFallback(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	calls := 0
	load := func(context.Context) ([]domain.Package, error) {
		calls++
		return []domain.Package{{ID: "pkg-test", Name: "Test", DataGB: 1, Days: 1, Price: 1000}}, nil
	}
	for range 2 {
		items, err := s.Packages(ctx, load)
		if err != nil || len(items) != 1 {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Fatal("cache miss on repeat")
	}
	ttl, err := s.Client.TTL(ctx, s.Prefix+"catalog:v1").Result()
	if err != nil || ttl <= 0 || ttl > 30*time.Second {
		t.Fatalf("unbounded TTL %v %v", ttl, err)
	}
	if err = s.Client.Set(ctx, s.Prefix+"catalog:v1", `[{"id":"wrong"}]`, time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Packages(ctx, load); err != nil || calls != 2 {
		t.Fatalf("corrupt cache did not fall back %v", err)
	}
	if err = s.Client.Del(ctx, s.Prefix+"catalog:v1").Err(); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Packages(ctx, func(context.Context) ([]domain.Package, error) { return nil, errors.New("catalog down") }); err == nil {
		t.Fatal("fabricated catalog")
	}
}
func TestRedisUnavailable(t *testing.T) {
	s, err := Open("redis://127.0.0.1:1/0", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Client.Close(); err != nil {
			t.Error(err)
		}
	})
	ctx := context.Background()
	_, err = s.Allow(ctx, "write", 1, time.Minute)
	if err == nil {
		t.Fatal("failed open")
	}
	items, err := s.Packages(ctx, func(context.Context) ([]domain.Package, error) { return []domain.Package{}, nil })
	if err != nil || items == nil {
		t.Fatalf("read fallback failed %v", err)
	}
}
