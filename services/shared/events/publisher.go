package events

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/twmb/franz-go/pkg/kgo"
	"go.opentelemetry.io/otel/codes"
)

// Send publishes one record and returns only after broker acknowledgment.
type Send func(context.Context, string, string, []byte) error

// KafkaSender adapts an acknowledged Kafka producer to the outbox worker.
func KafkaSender(client *kgo.Client) Send {
	return func(ctx context.Context, topic, key string, payload []byte) error {
		return client.ProduceSync(ctx, &kgo.Record{Topic: topic, Key: []byte(key), Value: payload, Headers: []kgo.RecordHeader{{Key: "traceparent", Value: []byte(TraceParent(ctx))}}}).FirstErr()
	}
}

// PublishOne locks a due event across replicas. A crash after broker ack can duplicate
// publication; consumers must deduplicate using event_id before committing offsets.
func PublishOne(ctx context.Context, pool *pgxpool.Pool, send Send) (bool, error) {
	return PublishOneFrom(ctx, pool, GatewayOutbox, send)
}

// PublishOneFrom relays a record from a validated service-owned outbox.
func PublishOneFrom(ctx context.Context, pool *pgxpool.Pool, outbox Outbox, send Send) (bool, error) {
	if !outbox.valid() {
		return false, errors.New("invalid outbox")
	}
	table := string(outbox)
	tx, err := pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if e := tx.Rollback(cleanup); e != nil && !errors.Is(e, pgx.ErrTxClosed) {
			slog.Error("release outbox lock", "error", e)
		}
	}()
	var id, topic, key, parent string
	var payload []byte
	err = tx.QueryRow(ctx, `SELECT event_id,topic,partition_key,payload,traceparent FROM `+table+` WHERE published_at IS NULL AND available_at<=now() ORDER BY created_at FOR UPDATE SKIP LOCKED LIMIT 1`).Scan(&id, &topic, &key, &payload, &parent)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	ctx, span := producerSpan(ctx, topic, parent)
	defer span.End()
	attempt, cancel := context.WithTimeout(ctx, 5*time.Second)
	sendErr := send(attempt, topic, key, payload)
	cancel()
	if sendErr != nil {
		span.SetStatus(codes.Error, "Kafka publication failed")
		_, err = tx.Exec(ctx, `UPDATE `+table+` SET attempts=attempts+1,last_error=$2,available_at=now()+least(60, power(2,least(attempts,6))) * interval '1 second' WHERE event_id=$1`, id, sendErr.Error())
	} else {
		_, err = tx.Exec(ctx, `UPDATE `+table+` SET published_at=now(),attempts=attempts+1,last_error='' WHERE event_id=$1`, id)
	}
	if err != nil {
		return false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, sendErr
}

// Publish runs a bounded sequential relay until cancellation; outages retain events.
func Publish(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger, send Send) {
	PublishFrom(ctx, pool, log, GatewayOutbox, send)
}

// PublishFrom runs one service's relay independently of other producers.
func PublishFrom(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger, outbox Outbox, send Send) {
	for ctx.Err() == nil {
		handled, err := PublishOneFrom(ctx, pool, outbox, send)
		if err != nil && ctx.Err() == nil {
			log.Warn("outbox publication deferred", "error", err)
		}
		if handled && err == nil {
			continue
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

// NewProducer uses bounded at-least-once publication; consumers deduplicate events.
func NewProducer(brokers string) (*kgo.Client, error) {
	if brokers == "" {
		brokers = "127.0.0.1:19092"
	}
	return kgo.NewClient(kgo.SeedBrokers(strings.Split(brokers, ",")...), kgo.RecordDeliveryTimeout(5*time.Second), kgo.DisableIdempotentWrite(), kgo.ProduceRequestTimeout(3*time.Second), kgo.RequestTimeoutOverhead(time.Second), kgo.AllowAutoTopicCreation())
}
