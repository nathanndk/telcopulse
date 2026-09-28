// Package subscriber owns synthetic subscriber profiles and masked identity lookup.
package subscriber

import (
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"log/slog"
	"net/http"
	"telcopulse/services/shared/domain"
	rt "telcopulse/services/shared/runtime"
)

// Handler exposes subscriber-owned read operations.
func Handler(pool *pgxpool.Pool, log *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /internal/customers", func(w http.ResponseWriter, r *http.Request) {
		rows, err := pool.Query(r.Context(), "SELECT id,name,msisdn FROM subscriber.profiles WHERE account_state='ACTIVE' ORDER BY id")
		if err != nil {
			log.Error("list subscribers", "error", err)
			rt.JSON(w, 503, map[string]string{"error": "subscriber unavailable"})
			return
		}
		defer rows.Close()
		out := []domain.Customer{}
		for rows.Next() {
			var c domain.Customer
			if err = rows.Scan(&c.ID, &c.Name, &c.MSISDN); err != nil {
				rt.JSON(w, 500, map[string]string{"error": "subscriber read failed"})
				return
			}
			c.MSISDN = domain.MaskMSISDN(c.MSISDN)
			out = append(out, c)
		}
		if err = rows.Err(); err != nil {
			rt.JSON(w, 500, map[string]string{"error": "subscriber read failed"})
			return
		}
		rt.JSON(w, 200, out)
	})
	mux.HandleFunc("GET /internal/customers/{id}", func(w http.ResponseWriter, r *http.Request) {
		var c domain.Customer
		err := pool.QueryRow(r.Context(), "SELECT id,name,msisdn FROM subscriber.profiles WHERE id=$1 AND account_state='ACTIVE'", r.PathValue("id")).Scan(&c.ID, &c.Name, &c.MSISDN)
		if errors.Is(err, pgx.ErrNoRows) {
			rt.JSON(w, 404, map[string]string{"error": "subscriber not found"})
			return
		}
		if err != nil {
			rt.JSON(w, 503, map[string]string{"error": "subscriber unavailable"})
			return
		}
		c.MSISDN = domain.MaskMSISDN(c.MSISDN)
		log.Info("subscriber loaded", "transaction_id", r.Header.Get("X-Transaction-ID"), "trace_id", r.Header.Get("X-Trace-ID"), "msisdn_masked", c.MSISDN)
		rt.JSON(w, 200, c)
	})
	return mux
}
