package logexport

import (
	"context"
	"sync"
	"sync/atomic"
)

// Sender must honor context cancellation and must not retain the event buffer.
type Sender interface {
	Send(context.Context, []byte) error
}

// Queue keeps application log writes independent of remote receiver latency.
// It is best-effort: stdout remains the primary log stream, and failures are counted.
type Queue struct {
	mu                        sync.RWMutex
	closed                    bool
	records                   chan []byte
	done                      chan struct{}
	cancel                    context.CancelFunc
	accepted, failed, dropped atomic.Uint64
}

// Stats reports monotonic outcomes and a point-in-time buffered record count.
type Stats struct {
	Accepted, Failed, Dropped uint64
	Pending                   int
}

// NewQueue bounds buffered payload memory to 256 records of at most 64 KiB each.
func NewQueue(sender Sender) *Queue {
	ctx, cancel := context.WithCancel(context.Background())
	q := &Queue{records: make(chan []byte, 256), done: make(chan struct{}), cancel: cancel}
	go func() {
		defer close(q.done)
		defer cancel()
		for record := range q.records {
			if ctx.Err() != nil {
				q.dropped.Add(1)
				continue
			}
			if sender.Send(ctx, record) != nil {
				q.failed.Add(1)
			} else {
				q.accepted.Add(1)
			}
		}
	}()
	return q
}

// Write accepts one JSON log record per call. A copy prevents caller buffer reuse.
// Overflow never blocks the application; it is observable through Stats.
func (q *Queue) Write(record []byte) (int, error) {
	q.mu.RLock()
	defer q.mu.RUnlock()
	if q.closed || len(record) == 0 || len(record) > 65536 {
		q.dropped.Add(1)
		return len(record), nil
	}
	select {
	case q.records <- append([]byte(nil), record...):
	default:
		q.dropped.Add(1)
	}
	return len(record), nil
}

func (q *Queue) Stats() Stats {
	return Stats{Accepted: q.accepted.Load(), Failed: q.failed.Load(), Dropped: q.dropped.Load(), Pending: len(q.records)}
}

// Close drains queued logs until the deadline, then cancels remote delivery.
// Repeated or concurrent calls are safe. Late writes are counted as dropped.
func (q *Queue) Close(ctx context.Context) error {
	q.mu.Lock()
	if !q.closed {
		q.closed = true
		close(q.records)
	}
	q.mu.Unlock()
	select {
	case <-q.done:
		return nil
	case <-ctx.Done():
		q.cancel()
		return ctx.Err()
	}
}
