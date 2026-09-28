package workflow

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/twmb/franz-go/pkg/kgo"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"telcopulse/services/shared/ephemeral"
	"telcopulse/services/shared/events"
	"telcopulse/services/shared/telemetry"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"telcopulse/services/api-gateway/internal/store"
	notification "telcopulse/services/notification-service"
	packages "telcopulse/services/package-service"
	payment "telcopulse/services/payment-service"
	"telcopulse/services/shared/domain"
	"telcopulse/services/shared/rpc"
	rt "telcopulse/services/shared/runtime"
	subscriber "telcopulse/services/subscriber-service"
)

func setup(t *testing.T, wrapPayment, wrapPackage, wrapNotification func(http.Handler) http.Handler) *Service {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL required for distributed integration tests")
	}
	ctx := context.Background()
	db, err := store.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Pool.Close)
	_, err = db.Pool.Exec(ctx, `TRUNCATE payment.event_outbox,notification.event_outbox,event_outbox,notification.inbox,notification.dead_letter_replays,notification.dead_letters,purchase_workflows,notification.deliveries,package.activation_outcomes,package.activations,payment.reservations,audit_logs,entitlements,payments,transactions RESTART IDENTITY; UPDATE payment.accounts SET balance_idr=250000 WHERE customer_id='cus-001';UPDATE payment.accounts SET balance_idr=10000 WHERE customer_id='cus-003'`)
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewJSONHandler(io.Discard, nil))
	token := "integration-only-service-token-not-a-real-secret"
	serve := func(h http.Handler, wrap func(http.Handler) http.Handler) *rpc.Client {
		if wrap != nil {
			h = wrap(h)
		}
		server := httptest.NewServer(rt.Protect(h, token))
		t.Cleanup(server.Close)
		return rpc.New(server.URL, token)
	}
	return &Service{Metrics: telemetry.New("api-gateway", nil), Store: db, Subscriber: serve(subscriber.Handler(db.Pool, log), nil), Package: serve(packages.Handler(db.Pool, log), wrapPackage), Payment: serve(payment.Handler(db.Pool, log), wrapPayment), Log: log}
}
func purchaseInput() domain.Purchase {
	return domain.Purchase{CustomerID: "cus-001", PackageID: "pkg-10", PaymentMethod: "Pulsa", Environment: "development"}
}
func count(t *testing.T, s *Service, table string) int {
	t.Helper()
	var n int
	if err := s.Pool.QueryRow(context.Background(), "SELECT count(*) FROM "+table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
func balance(t *testing.T, s *Service) int {
	t.Helper()
	var n int
	if err := s.Pool.QueryRow(context.Background(), "SELECT balance_idr FROM payment.accounts WHERE customer_id='cus-001'").Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
func TestDistributedPurchase(t *testing.T) {
	s := setup(t, nil, nil, nil)
	ctx := context.Background()
	p := purchaseInput()
	first, replayed, err := s.Purchase(ctx, p, "distributed-purchase-key")
	if err != nil {
		t.Fatal(err)
	}
	if replayed || first.Status != "SUCCESS" || len(first.Steps) != 6 {
		t.Fatalf("unexpected result %+v", first)
	}
	second, replayed, err := s.Purchase(ctx, p, "distributed-purchase-key")
	if err != nil || !replayed || first.ID != second.ID {
		t.Fatalf("replay failed %v", err)
	}
	for _, table := range []string{"transactions", "payment.reservations", "package.activations", "event_outbox", "audit_logs"} {
		if n := count(t, s, table); n != 1 {
			t.Fatalf("%s has %d records", table, n)
		}
	}
	if businessCount(t, s, "SUCCESS") != 1 {
		t.Fatal("replay double-counted business metric")
	}
	if count(t, s, "payment.event_outbox") != 2 {
		t.Fatal("payment transitions missing or duplicated")
	}
	if balance(t, s) != 200000 {
		t.Fatal("duplicate debit")
	}
	p.PackageID = "pkg-3"
	if _, _, err = s.Purchase(ctx, p, "distributed-purchase-key"); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("expected conflict: %v", err)
	}
	p.CustomerID = "cus-003"
	p.PackageID = "pkg-25"
	failed, _, err := s.Purchase(ctx, p, "distributed-failure-key")
	if err != nil || failed.Status != "FAILED" || failed.ErrorCode != "INSUFFICIENT_BALANCE" {
		t.Fatalf("failed purchase %+v %v", failed, err)
	}
	if businessCount(t, s, "FAILED") != 1 {
		t.Fatal("HTTP success hid business failure metric")
	}
	if count(t, s, "package.activations") != 1 {
		t.Fatal("failure activated package")
	}
	overview, err := s.Overview(ctx, "development")
	if err != nil || overview.Total != 2 || overview.SuccessRate != 50 {
		t.Fatalf("business metrics %+v %v", overview, err)
	}
	page, err := s.Transactions(ctx, "development", "FAILED", "Citra", 1, 20)
	if err != nil || page.Total != 1 {
		t.Fatalf("filter %+v %v", page, err)
	}
	p.CustomerID = "missing"
	if _, _, err = s.Purchase(ctx, p, "missing-subscriber-key"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected missing subscriber: %v", err)
	}
}

func TestLinkedFailedPurchaseReplay(t *testing.T) {
	s := setup(t, nil, nil, nil)
	ctx := context.Background()
	p := domain.Purchase{CustomerID: "cus-003", PackageID: "pkg-25", PaymentMethod: "Pulsa", Environment: "development"}
	failed, _, err := s.Purchase(ctx, p, "linked-replay-source-key")
	if err != nil || failed.Status != "FAILED" || failed.ErrorCode != "INSUFFICIENT_BALANCE" {
		t.Fatalf("source failure %+v %v", failed, err)
	}
	p.ReplayOf = failed.ID
	replayed, duplicate, err := s.Purchase(ctx, p, "linked-replay-attempt-key")
	if err != nil || duplicate || replayed.Status != "FAILED" || replayed.ID == failed.ID || replayed.ReplayOf != failed.ID || replayed.TraceID == failed.TraceID {
		t.Fatalf("linked attempt %+v duplicate=%v err=%v", replayed, duplicate, err)
	}
	persisted, err := s.Transaction(ctx, replayed.ID)
	if err != nil || persisted.ReplayOf != failed.ID {
		t.Fatalf("lineage not persisted %+v %v", persisted, err)
	}
	again, duplicate, err := s.Purchase(ctx, p, "linked-replay-attempt-key")
	if err != nil || !duplicate || again.ID != replayed.ID {
		t.Fatalf("network replay changed linked attempt %+v duplicate=%v err=%v", again, duplicate, err)
	}
	var customerBalance int
	if err := s.Pool.QueryRow(ctx, "SELECT balance_idr FROM payment.accounts WHERE customer_id='cus-003'").Scan(&customerBalance); err != nil {
		t.Fatal(err)
	}
	if count(t, s, "transactions") != 2 || count(t, s, "event_outbox") != 2 || count(t, s, "audit_logs") != 2 || customerBalance != 10000 {
		t.Fatal("linked replay duplicated an outcome or debited the failed purchase customer")
	}
	for name, invalid := range map[string]domain.Purchase{
		"different customer":    {CustomerID: "cus-001", PackageID: "pkg-25", PaymentMethod: "Pulsa", Environment: "development", ReplayOf: failed.ID},
		"different package":     {CustomerID: "cus-003", PackageID: "pkg-10", PaymentMethod: "Pulsa", Environment: "development", ReplayOf: failed.ID},
		"different payment":     {CustomerID: "cus-003", PackageID: "pkg-25", PaymentMethod: "E-Wallet", Environment: "development", ReplayOf: failed.ID},
		"different environment": {CustomerID: "cus-003", PackageID: "pkg-25", PaymentMethod: "Pulsa", Environment: "staging", ReplayOf: failed.ID},
		"missing source":        {CustomerID: "cus-003", PackageID: "pkg-25", PaymentMethod: "Pulsa", Environment: "development", ReplayOf: "TXN-000000000000000000000000"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := s.Purchase(ctx, invalid, "invalid-replay-"+name); !errors.Is(err, ErrInvalidReplay) {
				t.Fatalf("invalid replay accepted: %v", err)
			}
		})
	}
	if count(t, s, "transactions") != 2 {
		t.Fatal("invalid replay caused a purchase")
	}
	healthy, _, err := s.Purchase(ctx, domain.Purchase{CustomerID: "cus-001", PackageID: "pkg-10", PaymentMethod: "E-Wallet", Environment: "development"}, "linked-replay-healthy-key")
	if err != nil || healthy.Status != "SUCCESS" {
		t.Fatalf("healthy source %+v %v", healthy, err)
	}
	if _, _, err := s.Purchase(ctx, domain.Purchase{CustomerID: "cus-001", PackageID: "pkg-10", PaymentMethod: "E-Wallet", Environment: "development", ReplayOf: healthy.ID}, "invalid-replay-healthy-key"); !errors.Is(err, ErrInvalidReplay) {
		t.Fatalf("healthy source was replayed: %v", err)
	}
}
func TestLostPaymentResponse(t *testing.T) {
	var once atomic.Bool
	s := setup(t, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/internal/reserve" && once.CompareAndSwap(false, true) {
				rec := httptest.NewRecorder()
				next.ServeHTTP(rec, r)
				http.Error(w, "response lost after commit", http.StatusServiceUnavailable)
				return
			}
			next.ServeHTTP(w, r)
		})
	}, nil, nil)
	ctx := context.Background()
	_, _, err := s.Purchase(ctx, purchaseInput(), "lost-payment-response")
	if !errors.Is(err, ErrPending) {
		t.Fatalf("expected pending %v", err)
	}
	if balance(t, s) != 200000 {
		t.Fatal("reservation missing")
	}
	pending, e := s.Transactions(ctx, "development", "PROCESSING", "", 1, 20)
	if e != nil || pending.Total != 1 || pending.Items[0].Status != "PROCESSING" {
		t.Fatalf("pending purchase not visible %+v %v", pending, e)
	}
	overview, e := s.Overview(ctx, "development")
	if e != nil || overview.Total != 0 {
		t.Fatalf("pending counted as completed %+v %v", overview, e)
	}
	// A new coordinator instance resumes the same durable identity, like a gateway restart.
	restarted := *s
	result, _, err := restarted.Purchase(ctx, purchaseInput(), "lost-payment-response")
	if err != nil || result.Status != "SUCCESS" {
		t.Fatalf("recovery %+v %v", result, err)
	}
	if balance(t, s) != 200000 || count(t, s, "payment.reservations") != 1 {
		t.Fatal("recovery double charged")
	}
}
func TestActivationRejectionReleasesPayment(t *testing.T) {
	s := setup(t, nil, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/internal/activate" {
				rt.JSON(w, 200, domain.OperationResult{Status: "FAILED", ErrorCode: "PACKAGE_ACTIVATION_FAILED"})
				return
			}
			next.ServeHTTP(w, r)
		})
	}, nil)
	result, _, err := s.Purchase(context.Background(), purchaseInput(), "activation-rejection")
	if err != nil || result.Status != "FAILED" {
		t.Fatalf("result %+v %v", result, err)
	}
	if balance(t, s) != 250000 || count(t, s, "package.activations") != 0 {
		t.Fatal("failed activation retained charge or entitlement")
	}
}
func TestConcurrentPurchases(t *testing.T) {
	for _, sameKey := range []bool{true, false} {
		t.Run(fmt.Sprint(sameKey), func(t *testing.T) {
			s := setup(t, nil, nil, nil)
			p := purchaseInput()
			p.PackageID = "pkg-25"
			var wg sync.WaitGroup
			errs := make(chan error, 4)
			for i := range 4 {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					key := fmt.Sprintf("concurrent-key-%d", i)
					if sameKey {
						key = "same-concurrent-key"
					}
					_, _, err := s.Purchase(context.Background(), p, key)
					if err != nil && !errors.Is(err, ErrPending) {
						errs <- err
					}
				}(i)
			}
			wg.Wait()
			close(errs)
			for err := range errs {
				t.Error(err)
			}
			expected := 50000
			if sameKey {
				expected = 150000
			}
			if balance(t, s) != expected {
				t.Fatalf("balance=%d want=%d", balance(t, s), expected)
			}
		})
	}
}
func TestNotificationOutboxRecovery(t *testing.T) {
	s := setup(t, nil, nil, nil)
	ctx := context.Background()
	result, _, err := s.Purchase(ctx, purchaseInput(), "notification-recovery")
	if err != nil || result.Status != "SUCCESS" {
		t.Fatalf("purchase must not wait for broker: %+v %v", result, err)
	}
	if count(t, s, "notification.deliveries") != 0 {
		t.Fatal("notification bypassed Kafka")
	}
	handled, err := events.PublishOne(ctx, s.Pool, func(context.Context, string, string, []byte) error { return errors.New("broker offline") })
	if !handled || err == nil {
		t.Fatal("outage not recorded")
	}
	var published bool
	if err = s.Pool.QueryRow(ctx, "SELECT published_at IS NOT NULL FROM event_outbox").Scan(&published); err != nil || published {
		t.Fatalf("lost event %v", err)
	}
	if _, err = s.Pool.Exec(ctx, "UPDATE event_outbox SET available_at=now()"); err != nil {
		t.Fatal(err)
	}
	var message []byte
	handled, err = events.PublishOne(ctx, s.Pool, func(_ context.Context, topic, key string, payload []byte) error {
		message = payload
		if topic != events.PurchaseTopic || key != result.ID {
			return errors.New("wrong routing")
		}
		return nil
	})
	if !handled || err != nil {
		t.Fatalf("publish %v", err)
	}
	// Duplicate delivery models a consumer crash between database commit and offset commit.
	for range 2 {
		if err = notification.Deliver(ctx, s.Pool, message); err != nil {
			t.Fatal(err)
		}
	}
	if count(t, s, "notification.deliveries") != 1 || count(t, s, "notification.inbox") != 1 || count(t, s, "notification.event_outbox") != 1 || balance(t, s) != 200000 {
		t.Fatal("duplicate side effect")
	}
	var event events.PurchaseCompleted
	if err = json.Unmarshal(message, &event); err != nil {
		t.Fatal(err)
	}
	event.Operation.Amount++
	conflicting, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	if err = notification.Deliver(ctx, s.Pool, conflicting); err == nil {
		t.Fatal("conflicting identity accepted")
	}
	for i, payload := range [][]byte{conflicting, []byte("invalid json")} {
		record := &kgo.Record{Topic: events.PurchaseTopic, Partition: 0, Offset: int64(i), Value: payload}
		if err = notification.Process(ctx, s.Pool, record); err != nil {
			t.Fatal(err)
		}
	}
	if count(t, s, "notification.dead_letters") != 2 {
		t.Fatal("poison records lost")
	}
	// Already acknowledged publications are not republished.
	handled, err = events.PublishOne(ctx, s.Pool, func(context.Context, string, string, []byte) error { t.Fatal("unexpected publish"); return nil })
	if handled || err != nil {
		t.Fatal("published event still due")
	}
}

func TestNotificationDeadLetterOutbox(t *testing.T) {
	s := setup(t, nil, nil, nil)
	ctx := context.Background()
	record := &kgo.Record{Topic: events.PurchaseTopic, Partition: 2, Offset: 17, Value: []byte(`{"msisdn":"628123450123","broken":true}`)}
	for range 2 {
		if err := notification.Process(ctx, s.Pool, record); err != nil {
			t.Fatal(err)
		}
	}
	if count(t, s, "notification.dead_letters") != 1 || count(t, s, "notification.event_outbox") != 1 {
		t.Fatal("duplicate poison processing created more than one logical dead-letter event")
	}
	var topic string
	var payload []byte
	if err := s.Pool.QueryRow(ctx, "SELECT topic,payload FROM notification.event_outbox").Scan(&topic, &payload); err != nil {
		t.Fatal(err)
	}
	if topic != events.DeadLetterTopic {
		t.Fatalf("wrong dead-letter topic %q", topic)
	}
	var body map[string]any
	if err := json.Unmarshal(payload, &body); err != nil {
		t.Fatal(err)
	}
	if body["source_topic"] != events.PurchaseTopic || body["source_partition"] != float64(2) || body["source_offset"] != float64(17) || body["payload_bytes"] != float64(len(record.Value)) || body["payload_sha256"] == "" || body["reason"] != "invalid purchase event" {
		t.Fatalf("incomplete dead-letter metadata: %+v", body)
	}
	if _, ok := body["msisdn"]; ok {
		t.Fatal("raw subscriber data escaped into dead-letter topic")
	}
	handled, err := events.PublishOneFrom(ctx, s.Pool, events.NotificationOutbox, func(context.Context, string, string, []byte) error { return errors.New("broker offline") })
	if !handled || err == nil {
		t.Fatal("offline dead-letter publication did not retain retry")
	}
	var published bool
	if err := s.Pool.QueryRow(ctx, "SELECT published_at IS NOT NULL FROM notification.event_outbox").Scan(&published); err != nil || published {
		t.Fatal("offline dead-letter event was marked published")
	}
	if _, err := s.Pool.Exec(ctx, "UPDATE notification.event_outbox SET available_at=now()"); err != nil {
		t.Fatal(err)
	}
	handled, err = events.PublishOneFrom(ctx, s.Pool, events.NotificationOutbox, func(_ context.Context, sentTopic, key string, sent []byte) error {
		if sentTopic != events.DeadLetterTopic || key != events.PurchaseTopic+":2" || string(sent) != string(payload) {
			t.Errorf("wrong dead-letter publication topic=%q key=%q payload=%s", sentTopic, key, sent)
		}
		return nil
	})
	if !handled || err != nil {
		t.Fatalf("dead-letter retry failed: %v", err)
	}
	if err := s.Pool.QueryRow(ctx, "SELECT published_at IS NOT NULL FROM notification.event_outbox").Scan(&published); err != nil || !published {
		t.Fatal("dead-letter event was not acknowledged")
	}
}

func TestNotificationDeadLetterRegister(t *testing.T) {
	s := setup(t, nil, nil, nil)
	ctx := context.Background()
	for offset := int64(10); offset < 13; offset++ {
		record := &kgo.Record{Topic: events.PurchaseTopic, Partition: 1, Offset: offset, Value: []byte(fmt.Sprintf(`{"invalid_probe":"private-%d"}`, offset))}
		if err := notification.Process(ctx, s.Pool, record); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Pool.Exec(ctx, "DELETE FROM notification.event_outbox WHERE event_id=$1", "notification.dead-letter:"+events.PurchaseTopic+":1:10"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Pool.Exec(ctx, "UPDATE notification.event_outbox SET published_at=now() WHERE event_id=$1", "notification.dead-letter:"+events.PurchaseTopic+":1:12"); err != nil {
		t.Fatal(err)
	}
	token := "dead-letter-register-test-token-123456"
	server := httptest.NewServer(rt.Protect(notification.Handler(s.Pool, s.Log), token))
	defer server.Close()
	read := func(path string, expected int) ([]byte, map[string]any) {
		t.Helper()
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Authorization", "Bearer "+token)
		response, err := server.Client().Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != expected {
			t.Fatalf("status=%d expected=%d body=%s", response.StatusCode, expected, body)
		}
		var parsed map[string]any
		if err := json.Unmarshal(body, &parsed); err != nil {
			t.Fatal(err)
		}
		return body, parsed
	}
	base := "/internal/notifications/dead-letters"
	body, first := read(base+"?limit=2&reason=invalid+purchase+event", 200)
	if strings.Contains(string(body), "private-") || first["more"] != true {
		t.Fatal("raw poison payload escaped or first page was incomplete")
	}
	items, ok := first["items"].([]any)
	if !ok || len(items) != 2 {
		t.Fatalf("first page %+v", first)
	}
	seen := map[float64]bool{}
	states := map[string]bool{}
	for _, value := range items {
		item := value.(map[string]any)
		seen[item["source_offset"].(float64)] = true
		states[item["publication"].(string)] = true
	}
	cursor, ok := first["next"].(string)
	if !ok || cursor == "" {
		t.Fatal("missing keyset cursor")
	}
	_, second := read(base+"?limit=2&reason=invalid+purchase+event&cursor="+cursor, 200)
	secondItems, ok := second["items"].([]any)
	if !ok || len(secondItems) != 1 || second["more"] != false {
		t.Fatalf("second page %+v", second)
	}
	for _, value := range secondItems {
		item := value.(map[string]any)
		offset := item["source_offset"].(float64)
		if seen[offset] {
			t.Fatalf("duplicate source offset %.0f across pages", offset)
		}
		seen[offset] = true
		states[item["publication"].(string)] = true
	}
	if len(seen) != 3 || !states["PUBLISHED"] || !states["PENDING"] || !states["UNTRACKED"] {
		t.Fatalf("register lost rows or publication states: seen=%v states=%v", seen, states)
	}
	read(base+"?reason=event+identity+conflict&cursor="+cursor, 422)
	read(base+"?cursor=broken", 422)
	read(base+"?raw=true", 400)
}

func TestNotificationDeadLetterReplay(t *testing.T) {
	s := setup(t, nil, nil, nil)
	ctx := context.Background()
	result, _, err := s.Purchase(ctx, purchaseInput(), "dead-letter-replay-source")
	if err != nil || result.Status != "SUCCESS" {
		t.Fatalf("create purchase event: %+v %v", result, err)
	}
	var valid []byte
	if err := s.Pool.QueryRow(ctx, "SELECT payload FROM event_outbox WHERE partition_key=$1", result.ID).Scan(&valid); err != nil {
		t.Fatal(err)
	}
	for _, entry := range []struct {
		offset  int64
		payload []byte
	}{{200, valid}, {201, []byte("still invalid")}} {
		if _, err := s.Pool.Exec(ctx, `INSERT INTO notification.dead_letters(topic,partition_id,message_offset,payload,reason) VALUES($1,0,$2,$3,'invalid purchase event')`, events.PurchaseTopic, entry.offset, entry.payload); err != nil {
			t.Fatal(err)
		}
	}
	actor := "operator:USR-0123456789abcdef01234567"
	var wg sync.WaitGroup
	outcomes := make(chan notification.ReplayResult, 8)
	errorsSeen := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, _, replayErr := notification.Replay(ctx, s.Pool, 0, 200, "same-dead-letter-replay-key", actor)
			outcomes <- out
			errorsSeen <- replayErr
		}()
	}
	wg.Wait()
	close(outcomes)
	close(errorsSeen)
	for replayErr := range errorsSeen {
		if replayErr != nil {
			t.Fatalf("concurrent replay: %v", replayErr)
		}
	}
	for out := range outcomes {
		if out.Status != "DELIVERED" || out.TransactionID != result.ID || out.Actor != actor {
			t.Fatalf("unexpected replay result %+v", out)
		}
	}
	if count(t, s, "notification.deliveries") != 1 || count(t, s, "notification.event_outbox") != 1 || count(t, s, "notification.dead_letter_replays") != 1 {
		t.Fatal("concurrent replay duplicated delivery, fact or audit")
	}
	again, duplicate, err := notification.Replay(ctx, s.Pool, 0, 200, "second-dead-letter-replay-key", actor)
	if err != nil || duplicate || again.Status != "ALREADY_DELIVERED" || again.TransactionID != result.ID {
		t.Fatalf("new command did not report existing receipt: %+v duplicate=%v err=%v", again, duplicate, err)
	}
	invalid, duplicate, err := notification.Replay(ctx, s.Pool, 0, 201, "invalid-dead-letter-replay-key", actor)
	if err != nil || duplicate || invalid.Status != "REJECTED" || invalid.Reason != "invalid purchase event" || invalid.TransactionID != "" {
		t.Fatalf("invalid bytes were not rejected: %+v duplicate=%v err=%v", invalid, duplicate, err)
	}
	var event events.PurchaseCompleted
	if err := json.Unmarshal(valid, &event); err != nil {
		t.Fatal(err)
	}
	event.Operation.Amount++
	conflicting, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Pool.Exec(ctx, `INSERT INTO notification.dead_letters(topic,partition_id,message_offset,payload,reason) VALUES($1,0,202,$2,'event identity conflict')`, events.PurchaseTopic, conflicting); err != nil {
		t.Fatal(err)
	}
	conflict, _, err := notification.Replay(ctx, s.Pool, 0, 202, "conflict-dead-letter-replay-key", actor)
	if err != nil || conflict.Status != "REJECTED" || conflict.Reason != "event identity conflict" {
		t.Fatalf("identity conflict was not rejected: %+v err=%v", conflict, err)
	}
	if count(t, s, "notification.deliveries") != 1 || count(t, s, "notification.event_outbox") != 1 || count(t, s, "notification.inbox") != 1 || count(t, s, "notification.dead_letters") != 3 || count(t, s, "notification.dead_letter_replays") != 4 {
		t.Fatal("rejected replay changed delivery or source quarantine")
	}
	history, err := notification.ReplayHistory(ctx, s.Pool, 0, 200, 1, "")
	if err != nil || !history.More || history.Next == "" || len(history.Items) != 1 || history.Items[0].Status != "ALREADY_DELIVERED" {
		t.Fatalf("first replay history page: %+v err=%v", history, err)
	}
	older, err := notification.ReplayHistory(ctx, s.Pool, 0, 200, 1, history.Next)
	if err != nil || older.More || len(older.Items) != 1 || older.Items[0].Status != "DELIVERED" {
		t.Fatalf("older replay history page: %+v err=%v", older, err)
	}
	encodedHistory, err := json.Marshal(history)
	if err != nil || strings.Contains(string(encodedHistory), "same-dead-letter-replay-key") || strings.Contains(string(encodedHistory), string(valid)) || strings.Contains(string(encodedHistory), "payload") {
		t.Fatalf("replay history exposed restricted data: %s err=%v", encodedHistory, err)
	}
	if _, err := notification.ReplayHistory(ctx, s.Pool, 0, 201, 1, history.Next); !errors.Is(err, notification.ErrInvalidReplayCursor) {
		t.Fatalf("cursor crossed source coordinates: %v", err)
	}
	if _, err := notification.ReplayHistory(ctx, s.Pool, 0, 200, 1, "broken"); !errors.Is(err, notification.ErrInvalidReplayCursor) {
		t.Fatalf("malformed cursor accepted: %v", err)
	}
	if _, err := notification.ReplayHistory(ctx, s.Pool, 0, 999, 1, ""); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("missing history source: %v", err)
	}
	if _, _, err := notification.Replay(ctx, s.Pool, 0, 201, "same-dead-letter-replay-key", actor); !errors.Is(err, notification.ErrReplayConflict) {
		t.Fatalf("reused key across sources: %v", err)
	}
	if _, _, err := notification.Replay(ctx, s.Pool, 0, 999, "missing-dead-letter-replay-key", actor); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("missing source: %v", err)
	}
	if _, err := s.Pool.Exec(ctx, `UPDATE notification.dead_letter_replays SET status='REJECTED'`); err == nil {
		t.Fatal("replay audit unexpectedly mutable")
	}
}

func TestNotificationDeadLetterReplayBoundary(t *testing.T) {
	s := setup(t, nil, nil, nil)
	ctx := context.Background()
	if _, err := s.Pool.Exec(ctx, `INSERT INTO notification.dead_letters(topic,partition_id,message_offset,payload,reason) VALUES($1,0,301,$2,'invalid purchase event')`, events.PurchaseTopic, []byte("private-replay-payload")); err != nil {
		t.Fatal(err)
	}
	serviceToken := "boundary-service-token-not-a-real-secret"
	mutationToken := "boundary-notification-mutation-token-only"
	server := httptest.NewServer(rt.Protect(notification.HandlerWithReplay(s.Pool, s.Log, mutationToken), serviceToken))
	defer server.Close()
	path := "/internal/notifications/dead-letters/0/301/replay"
	read := func(client *rpc.Client, actor, role, key string) rpc.Response {
		t.Helper()
		response, err := client.ExchangeAs(ctx, http.MethodPost, path, nil, key, actor, role)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(response.Body), "private-replay-payload") {
			t.Fatal("raw quarantine bytes escaped in response")
		}
		return response
	}
	shared := rpc.New(server.URL, serviceToken)
	actor := "operator:USR-0123456789abcdef01234567"
	if got := read(shared, actor, "Engineer", "boundary-replay-key-0001"); got.Status != 401 {
		t.Fatalf("shared token alone reached replay: %d", got.Status)
	}
	privileged := rpc.New(server.URL, serviceToken)
	privileged.MutationToken = mutationToken
	if got := read(privileged, actor, "Viewer", "boundary-replay-key-0001"); got.Status != 403 {
		t.Fatalf("Viewer reached replay: %d", got.Status)
	}
	if count(t, s, "notification.dead_letter_replays") != 0 {
		t.Fatal("unauthorized command was audited as executed")
	}
	first := read(privileged, actor, "Engineer", "boundary-replay-key-0001")
	if first.Status != 201 || !strings.Contains(string(first.Body), `"status":"REJECTED"`) {
		t.Fatalf("engineer replay result: %d %s", first.Status, first.Body)
	}
	history, err := shared.Exchange(ctx, http.MethodGet, "/internal/notifications/dead-letters/0/301/replays?limit=1", nil, "")
	if err != nil || history.Status != 200 || !strings.Contains(string(history.Body), `"status":"REJECTED"`) || strings.Contains(string(history.Body), "private-replay-payload") || strings.Contains(string(history.Body), "boundary-replay-key-0001") {
		t.Fatalf("read replay history: %d %s err=%v", history.Status, history.Body, err)
	}
	missing, err := shared.Exchange(ctx, http.MethodGet, "/internal/notifications/dead-letters/0/999/replays", nil, "")
	if err != nil || missing.Status != 404 {
		t.Fatalf("missing replay history source: %d err=%v", missing.Status, err)
	}
	again := read(privileged, actor, "Engineer", "boundary-replay-key-0001")
	if again.Status != 200 || string(again.Body) != string(first.Body) {
		t.Fatalf("idempotent response changed: %d %s", again.Status, again.Body)
	}
	if got := read(privileged, "operator:USR-ffffffffffffffffffffffff", "Administrator", "boundary-replay-key-0001"); got.Status != 409 {
		t.Fatalf("cross-actor key reuse was accepted: %d", got.Status)
	}
	if count(t, s, "notification.dead_letter_replays") != 1 || count(t, s, "notification.deliveries") != 0 {
		t.Fatal("boundary checks changed delivery or audit count")
	}
}

func TestNotificationReplayHistoryPaginationKeepsKeysPrivate(t *testing.T) {
	s := setup(t, nil, nil, nil)
	ctx := context.Background()
	if _, err := s.Pool.Exec(ctx, `INSERT INTO notification.dead_letters(topic,partition_id,message_offset,payload,reason) VALUES($1,0,401,$2,'invalid purchase event')`, events.PurchaseTopic, []byte("private-payload")); err != nil {
		t.Fatal(err)
	}
	keys := []string{"private-command-key-a", "private-command-key-b", "private-command-key-c"}
	for index, key := range keys {
		if _, err := s.Pool.Exec(ctx, `INSERT INTO notification.dead_letter_replays(idempotency_key,topic,partition_id,message_offset,actor,status,attempted_at) VALUES($1,$2,0,401,$3,'REJECTED','2026-09-28T00:00:00Z')`, key, events.PurchaseTopic, fmt.Sprintf("operator-%d", index)); err != nil {
			t.Fatal(err)
		}
	}
	first, err := notification.ReplayHistory(ctx, s.Pool, 0, 401, 2, "")
	if err != nil || len(first.Items) != 2 || !first.More || first.Next == "" {
		t.Fatalf("first page %+v: %v", first, err)
	}
	decoded, err := base64.RawURLEncoding.DecodeString(first.Next)
	if err != nil || strings.Contains(string(decoded), "private-command-key") || !strings.Contains(string(decoded), `"digest"`) {
		t.Fatalf("cursor exposes key or is invalid: %s, %v", decoded, err)
	}
	second, err := notification.ReplayHistory(ctx, s.Pool, 0, 401, 2, first.Next)
	if err != nil || len(second.Items) != 1 || second.More || second.Next != "" {
		t.Fatalf("second page %+v: %v", second, err)
	}
	seen := map[string]bool{}
	for _, item := range append(first.Items, second.Items...) {
		if seen[item.Actor] {
			t.Fatalf("duplicate history item %q", item.Actor)
		}
		seen[item.Actor] = true
	}
	for index := range keys {
		actor := fmt.Sprintf("operator-%d", index)
		if !seen[actor] {
			t.Fatalf("missing history item %q", actor)
		}
	}
	if _, err := notification.ReplayHistory(ctx, s.Pool, 0, 402, 2, first.Next); !errors.Is(err, notification.ErrInvalidReplayCursor) {
		t.Fatalf("cross-source cursor accepted: %v", err)
	}
}

func TestOutboxRollback(t *testing.T) {
	s := setup(t, nil, nil, nil)
	ctx := context.Background()
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	op := domain.Operation{TransactionID: "TXN-rollback-test", TraceID: "0123456789abcdef0123456789abcdef", CustomerID: "cus-001", PackageID: "pkg-10", Environment: "development", PaymentMethod: "Pulsa", Amount: 50000, Days: 30, Outcome: "SUCCESS"}
	err = events.Enqueue(ctx, tx, events.PurchaseCompleted{ID: "purchase.completed:" + op.TransactionID, Version: 1, Type: events.PurchaseTopic, OccurredAt: time.Now(), Operation: op})
	if err != nil {
		t.Fatal(err)
	}
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if count(t, s, "event_outbox") != 0 {
		t.Fatal("rolled-back event escaped")
	}
}

func TestServiceAuthentication(t *testing.T) {
	h := rt.Protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }), "test-token")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/internal/reserve", nil))
	if w.Code != 401 {
		t.Fatal("unauthenticated internal operation allowed")
	}
}

func TestActivationRejectionIsDurable(t *testing.T) {
	s := setup(t, nil, nil, nil)
	ctx := context.Background()
	op := domain.Operation{TransactionID: "TXN-missing-package-attempt", TraceID: "0123456789abcdef0123456789abcdef", CustomerID: "cus-001", PackageID: "missing-package", Environment: "development", PaymentMethod: "E-Wallet", Amount: 1000, Days: 7}
	var first, second domain.OperationResult
	if err := s.Package.Call(ctx, http.MethodPost, "/internal/activate", op, &first, op.TraceID, op.TransactionID); err != nil {
		t.Fatal(err)
	}
	if first.Status != "FAILED" {
		t.Fatalf("unexpected outcome %+v", first)
	}
	if _, err := s.Pool.Exec(ctx, "INSERT INTO package.catalog VALUES('missing-package','Late catalog entry',1,7,1000) ON CONFLICT DO NOTHING"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := s.Pool.Exec(ctx, "DELETE FROM package.catalog WHERE id='missing-package'"); err != nil {
			t.Error(err)
		}
	})
	if err := s.Package.Call(ctx, http.MethodPost, "/internal/activate", op, &second, op.TraceID, op.TransactionID); err != nil {
		t.Fatal(err)
	}
	if second != first || count(t, s, "package.activations") != 0 {
		t.Fatal("retry changed a committed failure into activation")
	}
}

func TestKafkaNotificationDelivery(t *testing.T) {
	broker := os.Getenv("TEST_KAFKA_BROKER")
	if broker == "" {
		t.Skip("TEST_KAFKA_BROKER required for real broker test")
	}
	s := setup(t, nil, nil, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	producer, err := events.NewProducer(broker)
	if err != nil {
		t.Fatal(err)
	}
	defer producer.Close()
	result, _, err := s.Purchase(ctx, purchaseInput(), "real-kafka-notification")
	if err != nil {
		t.Fatal(err)
	}
	topic := fmt.Sprintf("telcopulse.integration.%d", time.Now().UnixNano())
	handled, err := events.PublishOne(ctx, s.Pool, func(ctx context.Context, _ string, key string, payload []byte) error {
		return events.KafkaSender(producer)(ctx, topic, key, payload)
	})
	if !handled || err != nil {
		t.Fatalf("broker publication: %v", err)
	}
	consumer, err := kgo.NewClient(kgo.SeedBrokers(broker), kgo.ConsumerGroup(fmt.Sprintf("integration-%d", time.Now().UnixNano())), kgo.ConsumeTopics(topic), kgo.DisableAutoCommit(), kgo.BlockRebalanceOnPoll())
	if err != nil {
		t.Fatal(err)
	}
	defer consumer.Close()
	done := make(chan struct{})
	go func() { defer close(done); notification.Consume(ctx, s.Pool, s.Log, consumer) }()
	defer func() { cancel(); <-done }()
	for ctx.Err() == nil {
		var exists bool
		if err = s.Pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM notification.deliveries WHERE transaction_id=$1)", result.ID).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if exists {
			verifyDomainFacts(t, ctx, s, producer, broker, topic, result.ID)
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("Kafka notification did not arrive")
}

func TestPurchaseBypassesDisplayCache(t *testing.T) {
	redisURL := os.Getenv("TEST_REDIS_URL")
	if redisURL == "" {
		t.Skip("TEST_REDIS_URL required")
	}
	s := setup(t, nil, nil, nil)
	cache, err := ephemeral.Open(redisURL, s.Log)
	if err != nil {
		t.Fatal(err)
	}
	cache.Prefix = fmt.Sprintf("telcopulse-workflow-test:%d:", time.Now().UnixNano())
	t.Cleanup(func() {
		if err := cache.Client.Close(); err != nil {
			t.Error(err)
		}
	})
	s.Cache = cache
	ctx := context.Background()
	if _, err = s.Packages(ctx); err != nil {
		t.Fatal(err)
	}
	var price int64
	if err = s.Pool.QueryRow(ctx, "SELECT price_idr FROM package.catalog WHERE id='pkg-10'").Scan(&price); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := s.Pool.Exec(ctx, "UPDATE package.catalog SET price_idr=$1 WHERE id='pkg-10'", price); err != nil {
			t.Error(err)
		}
	})
	if _, err = s.Pool.Exec(ctx, "UPDATE package.catalog SET price_idr=$1 WHERE id='pkg-10'", price+1234); err != nil {
		t.Fatal(err)
	}
	result, _, err := s.Purchase(ctx, purchaseInput(), "fresh-price-purchase")
	if err != nil || result.Amount != price+1234 {
		t.Fatalf("purchase used stale display price %+v %v", result, err)
	}
}

func verifyDomainFacts(t *testing.T, ctx context.Context, s *Service, producer *kgo.Client, broker, topic, transactionID string) {
	t.Helper()
	for _, outbox := range []events.Outbox{events.PaymentOutbox, events.NotificationOutbox} {
		for {
			handled, err := events.PublishOneFrom(ctx, s.Pool, outbox, func(ctx context.Context, kind, key string, payload []byte) error {
				return events.KafkaSender(producer)(ctx, topic+"."+kind, key, payload)
			})
			if err != nil {
				t.Fatal(err)
			}
			if !handled {
				break
			}
		}
	}
	consumer, err := kgo.NewClient(kgo.SeedBrokers(broker), kgo.ConsumerGroup(topic+"-facts"), kgo.ConsumeTopics(topic+"."+events.PaymentTopic, topic+"."+events.NotificationTopic))
	if err != nil {
		t.Fatal(err)
	}
	defer consumer.Close()
	seen := map[string]bool{}
	for len(seen) < 3 && ctx.Err() == nil {
		fetch := consumer.PollRecords(ctx, 3)
		if errs := fetch.Errors(); len(errs) > 0 {
			t.Fatal(errs)
		}
		for _, r := range fetch.Records() {
			var e events.Change
			if err = json.Unmarshal(r.Value, &e); err != nil {
				t.Fatal(err)
			}
			if e.Operation.TransactionID != transactionID || e.Operation.TraceID == "" {
				t.Fatal("lost correlation")
			}
			if e.State == "DELIVERED" && e.CausationID != "purchase.completed:"+transactionID {
				t.Fatal("lost causation")
			}
			if e.State == "CONFIRMED" && e.Sequence != 2 {
				t.Fatal("invalid transition order")
			}
			seen[e.State] = true
		}
	}
	if !seen["RESERVED"] || !seen["CONFIRMED"] || !seen["DELIVERED"] {
		t.Fatalf("missing facts: %v", seen)
	}
}

func TestPaymentEventFailureRollsBackDebit(t *testing.T) {
	s := setup(t, nil, nil, nil)
	ctx := context.Background()
	op := domain.Operation{TransactionID: "TXN-event-rollback-test", TraceID: "0123456789abcdef0123456789abcdef", CustomerID: "cus-001", PackageID: "pkg-10", Environment: "development", PaymentMethod: "Pulsa", Amount: 50000, Days: 30}
	// Force outbox insertion failure; payment state must roll back with it.
	if _, err := s.Pool.Exec(ctx, "INSERT INTO payment.event_outbox(event_id,topic,partition_key,payload) VALUES($1,$2,$3,'{}')", "payment.state:"+op.TransactionID+":RESERVED", events.PaymentTopic, op.TransactionID); err != nil {
		t.Fatal(err)
	}
	var result domain.OperationResult
	err := s.Payment.Call(ctx, http.MethodPost, "/internal/reserve", op, &result, op.TraceID, op.TransactionID)
	if err == nil {
		t.Fatal("failed event insertion was ignored")
	}
	if balance(t, s) != 250000 || count(t, s, "payment.reservations") != 0 {
		t.Fatal("payment committed without event")
	}
}

func TestNotificationEventFailureRollsBackReceipt(t *testing.T) {
	s := setup(t, nil, nil, nil)
	ctx := context.Background()
	op := domain.Operation{TransactionID: "TXN-notice-rollback-test", TraceID: "0123456789abcdef0123456789abcdef", CustomerID: "cus-001", PackageID: "pkg-10", Environment: "development", PaymentMethod: "Pulsa", Amount: 50000, Days: 30, Outcome: "SUCCESS"}
	if _, err := s.Pool.Exec(ctx, "INSERT INTO notification.event_outbox(event_id,topic,partition_key,payload) VALUES($1,$2,$3,'{}')", "notification.delivered:"+op.TransactionID, events.NotificationTopic, op.TransactionID); err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(events.PurchaseCompleted{ID: "purchase.completed:" + op.TransactionID, Version: 1, Type: events.PurchaseTopic, OccurredAt: time.Now(), Operation: op})
	if err != nil {
		t.Fatal(err)
	}
	if err = notification.Deliver(ctx, s.Pool, payload); err == nil {
		t.Fatal("failed event insertion was ignored")
	}
	if count(t, s, "notification.inbox") != 0 || count(t, s, "notification.deliveries") != 0 {
		t.Fatal("receipt committed without event")
	}
}

func businessCount(t *testing.T, s *Service, status string) float64 {
	t.Helper()
	families, err := s.Metrics.Registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, family := range families {
		if family.GetName() != "transaction_total" {
			continue
		}
		for _, metric := range family.Metric {
			for _, label := range metric.Label {
				if label.GetName() == "status" && label.GetValue() == status && metric.GetCounter().GetValue() > 0 {
					return metric.GetCounter().GetValue()
				}
			}
		}
	}
	return 0
}
