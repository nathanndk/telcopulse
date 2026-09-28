package logexport

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type sendFunc func(context.Context, []byte) error

func (f sendFunc) Send(ctx context.Context, b []byte) error { return f(ctx, b) }

func TestQueueBoundsAndOwnership(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var first string
	q := NewQueue(sendFunc(func(ctx context.Context, b []byte) error {
		if first == "" {
			close(entered)
			<-release
			first = string(b)
		}
		return nil
	}))
	record := []byte(`{"message":"original"}`)
	_, _ = q.Write(record)
	<-entered
	record[0] = 'X'
	for range 300 {
		_, _ = q.Write([]byte(`{}`))
	}
	if s := q.Stats(); s.Pending != 256 || s.Dropped != 44 {
		t.Fatalf("bounds %+v", s)
	}
	close(release)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := q.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if first != `{"message":"original"}` {
		t.Fatal("retained caller buffer")
	}
	if s := q.Stats(); s.Accepted != 257 || s.Pending != 0 {
		t.Fatalf("drain %+v", s)
	}
	_, _ = q.Write([]byte(`{}`))
	if q.Stats().Dropped != 45 {
		t.Fatal("late write unaccounted")
	}
}

func TestQueueFailureAndShutdown(t *testing.T) {
	q := NewQueue(sendFunc(func(context.Context, []byte) error { return errors.New("receiver unavailable") }))
	_, _ = q.Write([]byte(`{}`))
	if err := q.Close(context.Background()); err != nil || q.Stats().Failed != 1 {
		t.Fatal("missing failure count")
	}
	entered := make(chan struct{})
	blocked := NewQueue(sendFunc(func(ctx context.Context, _ []byte) error { close(entered); <-ctx.Done(); return ctx.Err() }))
	_, _ = blocked.Write([]byte(`{}`))
	<-entered
	for range 5 {
		_, _ = blocked.Write([]byte(`{}`))
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if !errors.Is(blocked.Close(ctx), context.Canceled) {
		t.Fatal("shutdown deadline ignored")
	}
	deadline, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	if err := blocked.Close(deadline); err != nil {
		t.Fatal(err)
	}
	if s := blocked.Stats(); s.Failed != 1 || s.Dropped != 5 {
		t.Fatalf("cancellation %+v", s)
	}
}

func TestConcurrentWriteClose(t *testing.T) {
	q := NewQueue(sendFunc(func(context.Context, []byte) error { return nil }))
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 100 {
				_, _ = q.Write([]byte(`{}`))
			}
		}()
	}
	if err := q.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	s := q.Stats()
	if s.Accepted+s.Failed+s.Dropped != 1200 {
		t.Fatalf("lost accounting %+v", s)
	}
}
