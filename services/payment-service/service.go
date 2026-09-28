// Package payment owns balances and idempotent reserve/confirm/release operations.
package payment

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"log/slog"
	"net/http"
	"telcopulse/services/shared/domain"
	"telcopulse/services/shared/events"
	"telcopulse/services/shared/faults"
	rt "telcopulse/services/shared/runtime"
)

// Handler exposes payment-owned operations.
func Handler(pool *pgxpool.Pool, log *slog.Logger) http.Handler {
	return HandlerWithDecision(pool, log, nil)
}

// Decide is the configured, deadline-bounded simulation decision boundary.
type Decide func(context.Context, faults.Request) (faults.Decision, error)

// HandlerWithDecision applies durable fault decisions only to new reservations.
func HandlerWithDecision(pool *pgxpool.Pool, log *slog.Logger, decide Decide) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /internal/balances", func(w http.ResponseWriter, r *http.Request) {
		rows, err := pool.Query(r.Context(), "SELECT customer_id,balance_idr FROM payment.accounts")
		if err != nil {
			rt.JSON(w, 503, map[string]string{"error": "balances unavailable"})
			return
		}
		defer rows.Close()
		out := map[string]int64{}
		for rows.Next() {
			var id string
			var balance int64
			if err = rows.Scan(&id, &balance); err != nil {
				rt.JSON(w, 503, map[string]string{"error": "balance read failed"})
				return
			}
			out[id] = balance
		}
		if rows.Err() != nil {
			rt.JSON(w, 503, map[string]string{"error": "balance read failed"})
			return
		}
		rt.JSON(w, 200, out)
	})
	for _, action := range []string{"reserve", "confirm", "release"} {
		mux.HandleFunc("POST /internal/"+action, func(w http.ResponseWriter, r *http.Request) {
			var op domain.Operation
			if !rt.Decode(w, r, &op) {
				return
			}
			if !domain.ValidOperation(op) {
				rt.JSON(w, 422, map[string]string{"error": "invalid operation"})
				return
			}
			result, status, err := operateWithDecision(r.Context(), pool, action, op, decide)
			if err != nil {
				log.Error("payment operation", "action", action, "transaction_id", op.TransactionID, "trace_id", op.TraceID, "error", err)
				rt.JSON(w, status, map[string]string{"error": "payment operation could not be confirmed"})
				return
			}
			log.Info("payment operation", "action", action, "transaction_id", op.TransactionID, "trace_id", op.TraceID, "environment", op.Environment, "status", result.Status, "error_code", result.ErrorCode)
			rt.JSON(w, status, result)
		})
	}
	return mux
}
func operateWithDecision(ctx context.Context, pool *pgxpool.Pool, action string, op domain.Operation, decide Decide) (domain.OperationResult, int, error) {
	var result domain.OperationResult
	payload, err := json.Marshal(op)
	if err != nil {
		return result, 500, err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return result, 503, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,2))", op.TransactionID); err != nil {
		return result, 503, err
	}
	var same bool
	err = tx.QueryRow(ctx, "SELECT status,error_code,request=$2::jsonb FROM payment.reservations WHERE transaction_id=$1", op.TransactionID, payload).Scan(&result.Status, &result.ErrorCode, &same)
	if err == nil {
		if !same {
			return result, 409, errors.New("operation conflict")
		}
		if action == "reserve" || result.Status == "FAILED" {
			return result, 200, nil
		}
		if result.Status == "RESERVED" {
			next := "CONFIRMED"
			if action == "release" {
				next = "RELEASED"
				if op.PaymentMethod == "Pulsa" {
					if _, err = tx.Exec(ctx, "UPDATE payment.accounts SET balance_idr=balance_idr+$1 WHERE customer_id=$2", op.Amount, op.CustomerID); err != nil {
						return result, 503, err
					}
				}
			}
			if _, err = tx.Exec(ctx, "UPDATE payment.reservations SET status=$1 WHERE transaction_id=$2", next, op.TransactionID); err != nil {
				return result, 503, err
			}
			result.Status = next
			if err = events.EnqueuePayment(ctx, tx, op, result); err != nil {
				return result, 503, err
			}
		} else if (action == "confirm" && result.Status != "CONFIRMED") || (action == "release" && result.Status != "RELEASED") {
			return result, 409, errors.New("invalid payment transition")
		}
		if err = tx.Commit(ctx); err != nil {
			return result, 503, err
		}
		return result, 200, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return result, 503, err
	}
	if action != "reserve" {
		return result, 404, errors.New("reservation not found")
	}
	var decision faults.Decision
	if decide != nil {
		decision, err = decide(ctx, faults.Request{TransactionID: op.TransactionID, Environment: op.Environment})
		if err != nil {
			return result, 503, err
		}
	}
	if decision.Inject && decision.Scenario == "bad-deployment" && decision.DeploymentID != "" {
		trace.SpanFromContext(ctx).SetAttributes(attribute.String("deployment.id", decision.DeploymentID), attribute.String("service.version", decision.Version), attribute.String("simulation.run_id", decision.RunID))
	}
	if decision.Inject && decision.Scenario == "database-latency" {
		if decision.DelayMS < 100 || decision.DelayMS > 4500 {
			return result, 503, errors.New("invalid simulation delay")
		}
		if _, err = tx.Exec(ctx, "SELECT pg_sleep($1)", float64(decision.DelayMS)/1000); err != nil {
			return result, 503, err
		}
	}
	if decision.Inject && decision.Scenario == "database-timeout" {
		if err = simulateDatabaseTimeout(ctx, tx, decision.DelayMS); err != nil {
			return result, 503, err
		}
	}
	if decision.Inject && decision.Scenario != "" && decision.Scenario != "payment-decline" && decision.Scenario != "database-latency" && decision.Scenario != "kafka-consumer-lag" && decision.Scenario != "database-timeout" && decision.Scenario != "bad-deployment" {
		return result, 503, errors.New("unsupported simulation decision")
	}
	var balance int64
	if err = tx.QueryRow(ctx, "SELECT balance_idr FROM payment.accounts WHERE customer_id=$1 FOR UPDATE", op.CustomerID).Scan(&balance); err != nil {
		return result, 503, err
	}
	result.Status = "RESERVED"
	if decision.Inject && decision.Scenario == "database-timeout" {
		result.Status = "FAILED"
		result.ErrorCode = "DB_TIMEOUT"
	} else if decision.Inject && decision.Scenario == "bad-deployment" {
		if decision.DeploymentID == "" || decision.Version == "" {
			return result, 503, errors.New("invalid synthetic deployment decision")
		}
		result.Status = "FAILED"
		result.ErrorCode = "BAD_DEPLOYMENT_PAYMENT_FAILURE"
	} else if decision.Inject && (decision.Scenario == "" || decision.Scenario == "payment-decline") {
		result.Status = "FAILED"
		result.ErrorCode = "SIMULATED_PAYMENT_DECLINED"
	} else if op.PaymentMethod == "Pulsa" {
		if balance < op.Amount {
			result.Status = "FAILED"
			result.ErrorCode = "INSUFFICIENT_BALANCE"
		} else if _, err = tx.Exec(ctx, "UPDATE payment.accounts SET balance_idr=balance_idr-$1 WHERE customer_id=$2", op.Amount, op.CustomerID); err != nil {
			return result, 503, err
		}
	}
	if _, err = tx.Exec(ctx, "INSERT INTO payment.reservations(transaction_id,request,customer_id,amount_idr,method,status,error_code) VALUES($1,$2,$3,$4,$5,$6,$7)", op.TransactionID, payload, op.CustomerID, op.Amount, op.PaymentMethod, result.Status, result.ErrorCode); err != nil {
		return result, 503, err
	}
	if err = events.EnqueuePayment(ctx, tx, op, result); err != nil {
		return result, 503, err
	}
	if err = tx.Commit(ctx); err != nil {
		return result, 503, err
	}
	return result, 200, nil
}
