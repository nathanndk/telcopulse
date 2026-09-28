// Package packages owns catalog and idempotent package activation.
package packages

import (
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"log/slog"
	"net/http"
	"telcopulse/services/shared/domain"
	rt "telcopulse/services/shared/runtime"
)

// Handler exposes catalog lookup and durable activation.
func Handler(pool *pgxpool.Pool, log *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /internal/packages", func(w http.ResponseWriter, r *http.Request) {
		rows, err := pool.Query(r.Context(), "SELECT id,name,data_gb,days,price_idr FROM package.catalog ORDER BY price_idr")
		if err != nil {
			rt.JSON(w, 503, map[string]string{"error": "catalog unavailable"})
			return
		}
		defer rows.Close()
		out := []domain.Package{}
		for rows.Next() {
			var p domain.Package
			if err = rows.Scan(&p.ID, &p.Name, &p.DataGB, &p.Days, &p.Price); err != nil {
				rt.JSON(w, 500, map[string]string{"error": "catalog read failed"})
				return
			}
			out = append(out, p)
		}
		if rows.Err() != nil {
			rt.JSON(w, 500, map[string]string{"error": "catalog read failed"})
			return
		}
		rt.JSON(w, 200, out)
	})
	mux.HandleFunc("GET /internal/packages/{id}", func(w http.ResponseWriter, r *http.Request) {
		var p domain.Package
		err := pool.QueryRow(r.Context(), "SELECT id,name,data_gb,days,price_idr FROM package.catalog WHERE id=$1", r.PathValue("id")).Scan(&p.ID, &p.Name, &p.DataGB, &p.Days, &p.Price)
		if errors.Is(err, pgx.ErrNoRows) {
			rt.JSON(w, 404, map[string]string{"error": "package not found"})
			return
		}
		if err != nil {
			rt.JSON(w, 503, map[string]string{"error": "catalog unavailable"})
			return
		}
		rt.JSON(w, 200, p)
	})
	mux.HandleFunc("POST /internal/activate", func(w http.ResponseWriter, r *http.Request) {
		var op domain.Operation
		if !rt.Decode(w, r, &op) {
			return
		}
		if !domain.ValidOperation(op) {
			rt.JSON(w, 422, map[string]string{"error": "invalid operation"})
			return
		}
		payload, err := json.Marshal(op)
		if err != nil {
			rt.JSON(w, 500, map[string]string{"error": "encode operation"})
			return
		}
		tx, err := pool.Begin(r.Context())
		if err != nil {
			rt.JSON(w, 503, map[string]string{"error": "activation unavailable"})
			return
		}
		defer func() { _ = tx.Rollback(r.Context()) }()
		if _, err = tx.Exec(r.Context(), "SELECT pg_advisory_xact_lock(hashtextextended($1,1))", op.TransactionID); err != nil {
			rt.JSON(w, 503, map[string]string{"error": "activation unavailable"})
			return
		}
		var same bool
		var outcome domain.OperationResult
		err = tx.QueryRow(r.Context(), "SELECT request=$2::jsonb,status,error_code FROM package.activation_outcomes WHERE transaction_id=$1", op.TransactionID, payload).Scan(&same, &outcome.Status, &outcome.ErrorCode)
		if err == nil {
			if !same {
				rt.JSON(w, 409, map[string]string{"error": "operation conflict"})
				return
			}
			rt.JSON(w, 200, outcome)
			return
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			rt.JSON(w, 503, map[string]string{"error": "activation unavailable"})
			return
		}
		tag, err := tx.Exec(r.Context(), "INSERT INTO package.activations(transaction_id,request,customer_id,package_id,expires_at) SELECT $1,$2,$3,id,now()+make_interval(days=>days) FROM package.catalog WHERE id=$4", op.TransactionID, payload, op.CustomerID, op.PackageID)
		if err != nil {
			rt.JSON(w, 503, map[string]string{"error": "activation unavailable"})
			return
		}
		outcome = domain.OperationResult{Status: "SUCCESS"}
		if tag.RowsAffected() == 0 {
			outcome = domain.OperationResult{Status: "FAILED", ErrorCode: "PACKAGE_UNAVAILABLE"}
		}
		if _, err = tx.Exec(r.Context(), "INSERT INTO package.activation_outcomes(transaction_id,request,status,error_code) VALUES($1,$2,$3,$4)", op.TransactionID, payload, outcome.Status, outcome.ErrorCode); err != nil {
			rt.JSON(w, 503, map[string]string{"error": "activation unavailable"})
			return
		}

		if err = tx.Commit(r.Context()); err != nil {
			rt.JSON(w, 503, map[string]string{"error": "activation result unconfirmed"})
			return
		}
		log.Info("package activated", "transaction_id", op.TransactionID, "trace_id", op.TraceID, "environment", op.Environment, "status", outcome.Status)
		rt.JSON(w, 200, outcome)
	})
	return mux
}
