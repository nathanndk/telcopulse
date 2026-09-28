// Package workflow coordinates resumable purchases across independently committed services.
package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"telcopulse/services/api-gateway/internal/store"
	"telcopulse/services/shared/domain"
	"telcopulse/services/shared/ephemeral"
	"telcopulse/services/shared/events"
	"telcopulse/services/shared/rpc"
	"telcopulse/services/shared/telemetry"
)

// ErrPending means durable work will be retried; clients must retain their idempotency key.
var ErrPending = errors.New("purchase is pending recovery; retry with the same idempotency key")

// ErrInvalidReplay does not disclose whether a source transaction exists in
// another environment or has a different customer configuration.
var ErrInvalidReplay = errors.New("replay source must be a matching failed transaction")

// Service owns orchestration, while the embedded store owns completed transaction reads.
type Service struct {
	Metrics *telemetry.Metrics
	Cache   *ephemeral.Store
	*store.Store
	Subscriber, Package, Payment *rpc.Client
	Log                          *slog.Logger
}

// Customers aggregates subscriber-owned identity and payment-owned balances.
func (s *Service) Customers(ctx context.Context) ([]domain.Customer, error) {
	var customers []domain.Customer
	var balances map[string]int64
	if err := s.Subscriber.Call(ctx, http.MethodGet, "/internal/customers", nil, &customers, "", ""); err != nil {
		return nil, err
	}
	if err := s.Payment.Call(ctx, http.MethodGet, "/internal/balances", nil, &balances, "", ""); err != nil {
		return nil, err
	}
	for i := range customers {
		customers[i].Balance = balances[customers[i].ID]
	}
	return customers, nil
}

// Packages retrieves the package service's catalog.
func (s *Service) Packages(ctx context.Context) ([]domain.Package, error) {
	if s.Cache != nil {
		return s.Cache.Packages(ctx, s.loadPackages)
	}
	return s.loadPackages(ctx)
}
func (s *Service) loadPackages(ctx context.Context) ([]domain.Package, error) {
	var out []domain.Package
	err := s.Package.Call(ctx, http.MethodGet, "/internal/packages", nil, &out, "", "")
	return out, err
}

// Purchase establishes durable identity before remote side effects, then resumes the workflow.
func (s *Service) Purchase(ctx context.Context, p domain.Purchase, key string) (domain.Transaction, bool, error) {
	var result domain.Transaction
	if err := p.Validate(); err != nil {
		return result, false, err
	}
	body, err := json.Marshal(p)
	if err != nil {
		return result, false, err
	}
	// Preserve replay behavior for transactions from Phase 1 as well as completed workflows.
	hash := sha256.Sum256(body)
	var oldHash string
	var data []byte
	err = s.Pool.QueryRow(ctx, "SELECT request_hash,result FROM transactions WHERE idempotency_key=$1", key).Scan(&oldHash, &data)
	if err == nil {
		if oldHash != hex.EncodeToString(hash[:]) {
			return result, false, store.ErrConflict
		}
		err = json.Unmarshal(data, &result)
		return result, true, err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return result, false, err
	}
	// Resume an existing durable request even when a catalog is temporarily unavailable.
	var same bool
	err = s.Pool.QueryRow(ctx, "SELECT request=$2::jsonb FROM purchase_workflows WHERE idempotency_key=$1", key, body).Scan(&same)
	if err == nil {
		if !same {
			return result, false, store.ErrConflict
		}
		result, err = s.resume(ctx, key)
		return result, true, err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return result, false, err
	}
	if p.ReplayOf != "" {
		var sourceData []byte
		err = s.Pool.QueryRow(ctx, "SELECT result FROM transactions WHERE id=$1 AND environment=$2 AND status='FAILED'", p.ReplayOf, p.Environment).Scan(&sourceData)
		if errors.Is(err, pgx.ErrNoRows) {
			return result, false, ErrInvalidReplay
		}
		if err != nil {
			return result, false, err
		}
		var source domain.Transaction
		if err = json.Unmarshal(sourceData, &source); err != nil {
			return result, false, err
		}
		if source.ID != p.ReplayOf || source.Status != "FAILED" || source.CustomerID != p.CustomerID || source.PackageID != p.PackageID || source.PaymentMethod != p.PaymentMethod || source.Environment != p.Environment {
			return result, false, ErrInvalidReplay
		}
	}
	id, err := domain.NewID(12)
	if err != nil {
		return result, false, err
	}
	trace := telemetry.TraceID(ctx)
	if trace == "" {
		trace, err = domain.NewID(16)
		if err != nil {
			return result, false, err
		}
	}
	result = domain.Transaction{ID: "TXN-" + id, ReplayOf: p.ReplayOf, TraceID: trace, CustomerID: p.CustomerID, PackageID: p.PackageID, PaymentMethod: p.PaymentMethod, Environment: p.Environment, Status: "PROCESSING", CreatedAt: time.Now().UTC(), Steps: []domain.Step{}}
	var c domain.Customer
	var pkg domain.Package
	started := time.Now()
	if err = s.Subscriber.Call(ctx, http.MethodGet, "/internal/customers/"+url.PathEscape(p.CustomerID), nil, &c, result.TraceID, result.ID); err != nil {
		return result, false, catalogError(err)
	}
	span, err := domain.NewID(8)
	if err != nil {
		return result, false, err
	}
	result.Steps = append(result.Steps, domain.Step{Service: "subscriber-service", Operation: "Validate subscriber", Status: "SUCCESS", DurationMS: float64(time.Since(started).Microseconds()) / 1000, SpanID: span})
	started = time.Now()
	if err = s.Package.Call(ctx, http.MethodGet, "/internal/packages/"+url.PathEscape(p.PackageID), nil, &pkg, result.TraceID, result.ID); err != nil {
		return result, false, catalogError(err)
	}
	span, err = domain.NewID(8)
	if err != nil {
		return result, false, err
	}
	result.Steps = append(result.Steps, domain.Step{Service: "package-service", Operation: "Check package availability", Status: "SUCCESS", DurationMS: float64(time.Since(started).Microseconds()) / 1000, SpanID: span})
	result.CustomerName = c.Name
	result.MSISDN = c.MSISDN
	result.PackageName = pkg.Name
	result.Amount = pkg.Price
	op := domain.Operation{TransactionID: result.ID, TraceID: result.TraceID, CustomerID: c.ID, PackageID: pkg.ID, PaymentMethod: p.PaymentMethod, Environment: p.Environment, Amount: pkg.Price, Days: pkg.Days}
	operation, err := json.Marshal(op)
	if err != nil {
		return result, false, err
	}
	data, err = json.Marshal(result)
	if err != nil {
		return result, false, err
	}
	tag, err := s.Pool.Exec(ctx, "INSERT INTO purchase_workflows(idempotency_key,transaction_id,request,result,operation,traceparent) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(idempotency_key) DO NOTHING", key, result.ID, body, data, operation, events.TraceParent(ctx))
	if err != nil {
		return result, false, err
	}
	same = false
	if err = s.Pool.QueryRow(ctx, "SELECT request=$2::jsonb FROM purchase_workflows WHERE idempotency_key=$1", key, body).Scan(&same); err != nil {
		return result, false, err
	}
	if !same {
		return result, false, store.ErrConflict
	}
	result, err = s.resume(ctx, key)
	return result, tag.RowsAffected() == 0, err
}
func (s *Service) resume(ctx context.Context, key string) (domain.Transaction, error) {
	var result domain.Transaction
	lock, err := s.Pool.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if e := lock.Rollback(cleanup); e != nil && !errors.Is(e, pgx.ErrTxClosed) {
			s.Log.Error("release workflow lock", "error", e)
		}
	}()
	var acquired bool
	if err = lock.QueryRow(ctx, "SELECT pg_try_advisory_xact_lock(hashtextextended($1,3))", key).Scan(&acquired); err != nil {
		return result, err
	}
	if !acquired {
		var pending []byte
		if err = s.Pool.QueryRow(ctx, "SELECT result FROM purchase_workflows WHERE idempotency_key=$1", key).Scan(&pending); err != nil {
			return result, err
		}
		if err = json.Unmarshal(pending, &result); err != nil {
			return result, err
		}
		result.Status = "PROCESSING"
		return result, ErrPending
	}
	var data, request, operation []byte
	var state, origin string
	if err = s.Pool.QueryRow(ctx, "SELECT request,result,operation,state,traceparent FROM purchase_workflows WHERE idempotency_key=$1", key).Scan(&request, &data, &operation, &state, &origin); err != nil {
		return result, err
	}
	var p domain.Purchase
	var op domain.Operation
	if err = json.Unmarshal(data, &result); err != nil {
		return result, err
	}
	if state != "PROCESSING" {
		return result, nil
	}
	if err = json.Unmarshal(request, &p); err != nil {
		return result, err
	}
	if err = json.Unmarshal(operation, &op); err != nil {
		return result, err
	}
	ctx, attemptSpan := telemetry.WorkflowAttempt(ctx, origin, result.ID, result.Environment)
	defer attemptSpan.End()
	result, err = s.execute(ctx, key, p, result, op)
	attemptSpan.SetAttributes(attribute.String("transaction.outcome", result.Status))
	if err != nil {
		attemptSpan.SetStatus(codes.Error, "purchase awaiting recovery")
		cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if _, e := s.Pool.Exec(cleanup, "UPDATE purchase_workflows SET attempts=attempts+1,last_error=$2,updated_at=now() WHERE idempotency_key=$1", key, err.Error()); e != nil {
			s.Log.Error("record workflow retry", "error", e)
		}
		s.Log.Warn("purchase awaiting recovery", "transaction_id", result.ID, "correlation_trace_id", result.TraceID, "trace_id", telemetry.TraceID(ctx), "error", err)
		return result, ErrPending
	}
	return result, nil
}
func (s *Service) save(ctx context.Context, key string, result domain.Transaction, op domain.Operation) error {
	data, err := json.Marshal(result)
	if err != nil {
		return err
	}
	body, err := json.Marshal(op)
	if err != nil {
		return err
	}
	_, err = s.Pool.Exec(ctx, "UPDATE purchase_workflows SET result=$2,operation=$3,updated_at=now() WHERE idempotency_key=$1", key, data, body)
	return err
}
func (s *Service) execute(ctx context.Context, key string, p domain.Purchase, result domain.Transaction, op domain.Operation) (domain.Transaction, error) {
	step := func(client *rpc.Client, path, service, operation string, input any, output any) error {
		started := time.Now()
		err := client.Call(ctx, http.MethodPost, path, input, output, result.TraceID, result.ID)
		if err != nil {
			return err
		}
		id, err := domain.NewID(8)
		if err != nil {
			return err
		}
		status := "SUCCESS"
		if v, ok := output.(*domain.OperationResult); ok && v.Status == "FAILED" {
			status = "FAILED"
		}
		// Replace the stage on recovery rather than fabricating duplicate operations.
		evidence := domain.Step{Service: service, Operation: operation, Status: status, DurationMS: float64(time.Since(started).Microseconds()) / 1000, SpanID: id}
		found := false
		for i := range result.Steps {
			if result.Steps[i].Operation == operation {
				result.Steps[i] = evidence
				found = true
				break
			}
		}
		if !found {
			result.Steps = append(result.Steps, evidence)
		}
		return s.save(ctx, key, result, op)
	}

	var payment, activation domain.OperationResult
	if result.ErrorCode == "" {
		if err := step(s.Payment, "/internal/reserve", "payment-service", "Reserve synthetic payment", op, &payment); err != nil {
			return result, err
		}
		if payment.Status == "FAILED" {
			result.ErrorCode = payment.ErrorCode
			if err := s.save(ctx, key, result, op); err != nil {
				return result, err
			}
		} else if payment.Status != "RESERVED" && payment.Status != "CONFIRMED" {
			return result, fmt.Errorf("unexpected reservation state %s", payment.Status)
		}
	}
	if result.ErrorCode == "" {
		if err := step(s.Package, "/internal/activate", "package-service", "Activate data entitlement", op, &activation); err != nil {
			return result, err
		}
		if activation.Status == "FAILED" {
			result.ErrorCode = activation.ErrorCode
			if err := s.save(ctx, key, result, op); err != nil {
				return result, err
			}
		} else if activation.Status != "SUCCESS" {
			return result, errors.New("unexpected activation state")
		}
	}
	if result.ErrorCode != "" {
		if err := step(s.Payment, "/internal/release", "payment-service", "Release unsuccessful payment", op, &payment); err != nil {
			return result, err
		}
		result.Status = "FAILED"
	} else {
		if err := step(s.Payment, "/internal/confirm", "payment-service", "Confirm payment", op, &payment); err != nil {
			return result, err
		}
		if payment.Status != "CONFIRMED" {
			return result, errors.New("payment was not confirmed")
		}
		result.Status = "SUCCESS"
	}
	queueSpan, err := domain.NewID(8)
	if err != nil {
		return result, err
	}
	result.Steps = append(result.Steps, domain.Step{Service: "api-gateway", Operation: "Queue purchase notification", Status: "SUCCESS", SpanID: queueSpan})
	result.DurationMS = float64(time.Since(result.CreatedAt).Microseconds()) / 1000
	if err := s.complete(ctx, key, p, result, op); err != nil {
		return result, err
	}
	if s.Metrics != nil {
		s.Metrics.Business(result.Environment, result.Status, result.DurationMS/1000)
	}
	return result, nil
}
func (s *Service) complete(ctx context.Context, key string, p domain.Purchase, result domain.Transaction, op domain.Operation) error {
	body, err := json.Marshal(p)
	if err != nil {
		return err
	}
	hash := sha256.Sum256(body)
	data, err := json.Marshal(result)
	if err != nil {
		return err
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, "INSERT INTO transactions(id,trace_id,idempotency_key,request_hash,customer_id,package_id,environment,status,created_at,result) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT(id) DO NOTHING", result.ID, result.TraceID, key, hex.EncodeToString(hash[:]), result.CustomerID, result.PackageID, result.Environment, result.Status, result.CreatedAt, data); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "UPDATE purchase_workflows SET state=$2,result=$3,last_error='',updated_at=now() WHERE idempotency_key=$1", key, result.Status, data); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "INSERT INTO audit_logs(actor,action,resource,details) VALUES('local-operator','transaction.created',$1,$2)", result.ID, data); err != nil {
		return err
	}
	op.Outcome = result.Status
	if err = events.Enqueue(ctx, tx, events.PurchaseCompleted{ID: "purchase.completed:" + result.ID, Version: 1, Type: events.PurchaseTopic, OccurredAt: time.Now().UTC(), Operation: op}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Recover retries pending workflows serially with bounded deadlines until shutdown.
func (s *Service) Recover(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.recoverBatch(ctx)
		}
	}
}
func (s *Service) recoverBatch(ctx context.Context) {
	rows, err := s.Pool.Query(ctx, "SELECT idempotency_key FROM purchase_workflows WHERE state='PROCESSING' AND updated_at<now()-interval '5 seconds' ORDER BY updated_at LIMIT 20")
	if err != nil {
		s.Log.Error("load pending purchases", "error", err)
		return
	}
	keys := []string{}
	for rows.Next() {
		var key string
		if err = rows.Scan(&key); err != nil {
			break
		}
		keys = append(keys, key)
	}
	rows.Close()
	if err != nil || rows.Err() != nil {
		s.Log.Error("read pending purchases", "error", err)
		return
	}
	for _, key := range keys {
		if ctx.Err() != nil {
			return
		}
		attempt, cancel := context.WithTimeout(ctx, 15*time.Second)
		_, err = s.resume(attempt, key)
		cancel()
		if err != nil && !errors.Is(err, ErrPending) {
			s.Log.Warn("recover purchase", "error", err)
		}
	}
}

func catalogError(err error) error {
	var remote *rpc.Error
	if errors.As(err, &remote) && remote.Status == 404 {
		return store.ErrNotFound
	}
	return err
}
