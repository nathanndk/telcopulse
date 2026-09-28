// Package notification owns durable synthetic delivery records and Kafka consumption.
package notification

import (
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	rt "telcopulse/services/shared/runtime"
	"time"
)

var transactionIDPattern = regexp.MustCompile(`^TXN-[a-f0-9]{24}$`)
var replayKeyPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{16,100}$`)
var replayActorPattern = regexp.MustCompile(`^operator:USR-[a-f0-9]{24}$`)

// Handler exposes durable synthetic receipts without operator replay enabled.
func Handler(pool *pgxpool.Pool, log *slog.Logger) http.Handler {
	return HandlerWithReplay(pool, log, "")
}

// HandlerWithReplay adds a separately authorized operator reprocessing command.
func HandlerWithReplay(pool *pgxpool.Pool, log *slog.Logger, mutationToken string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /internal/notifications/dead-letters", listDeadLetters(pool, log))
	mux.HandleFunc("GET /internal/notifications/dead-letters/{partition}/{offset}/replays", func(w http.ResponseWriter, r *http.Request) {
		for name, values := range r.URL.Query() {
			if (name != "cursor" && name != "limit") || len(values) != 1 {
				rt.JSON(w, 400, map[string]string{"error": "invalid replay history filter"})
				return
			}
		}
		partition, err := strconv.ParseInt(r.PathValue("partition"), 10, 32)
		if err != nil || partition < 0 {
			rt.JSON(w, 400, map[string]string{"error": "invalid source partition"})
			return
		}
		offset, err := strconv.ParseInt(r.PathValue("offset"), 10, 64)
		if err != nil || offset < 0 {
			rt.JSON(w, 400, map[string]string{"error": "invalid source offset"})
			return
		}
		limit := 20
		if raw := r.URL.Query().Get("limit"); raw != "" {
			limit, err = strconv.Atoi(raw)
			if err != nil || limit < 1 || limit > 50 {
				rt.JSON(w, 422, map[string]string{"error": "invalid replay history page size"})
				return
			}
		}
		page, err := ReplayHistory(r.Context(), pool, int32(partition), offset, limit, r.URL.Query().Get("cursor"))
		if errors.Is(err, pgx.ErrNoRows) {
			rt.JSON(w, 404, map[string]string{"error": "dead letter not found"})
			return
		}
		if errors.Is(err, ErrInvalidReplayCursor) {
			rt.JSON(w, 422, map[string]string{"error": "invalid replay history cursor"})
			return
		}
		if err != nil {
			log.Error("read replay history", "error", err)
			rt.JSON(w, 503, map[string]string{"error": "replay history unavailable"})
			return
		}
		rt.JSON(w, 200, page)
	})
	mux.HandleFunc("POST /internal/notifications/dead-letters/{partition}/{offset}/replay", func(w http.ResponseWriter, r *http.Request) {
		actor, role := r.Header.Get("X-Operator-Actor"), r.Header.Get("X-Operator-Role")
		if actor == "" && role == "" {
			actor = "local-operator"
		} else if !replayActorPattern.MatchString(actor) || (role != "Engineer" && role != "Administrator") {
			rt.JSON(w, 403, map[string]string{"error": "role cannot replay a dead letter"})
			return
		}
		key := r.Header.Get("Idempotency-Key")
		if !replayKeyPattern.MatchString(key) {
			rt.JSON(w, 400, map[string]string{"error": "invalid replay key"})
			return
		}
		partition, err := strconv.ParseInt(r.PathValue("partition"), 10, 32)
		if err != nil || partition < 0 {
			rt.JSON(w, 400, map[string]string{"error": "invalid source partition"})
			return
		}
		offset, err := strconv.ParseInt(r.PathValue("offset"), 10, 64)
		if err != nil || offset < 0 {
			rt.JSON(w, 400, map[string]string{"error": "invalid source offset"})
			return
		}
		if r.ContentLength != 0 {
			rt.JSON(w, 400, map[string]string{"error": "replay takes no payload"})
			return
		}
		result, duplicate, err := Replay(r.Context(), pool, int32(partition), offset, key, actor)
		if errors.Is(err, pgx.ErrNoRows) {
			rt.JSON(w, 404, map[string]string{"error": "dead letter not found"})
			return
		}
		if errors.Is(err, ErrReplayConflict) {
			rt.JSON(w, 409, map[string]string{"error": "replay key belongs to a different command"})
			return
		}
		if err != nil {
			log.Error("replay dead letter", "error", err)
			rt.JSON(w, 503, map[string]string{"error": "dead-letter replay unavailable"})
			return
		}
		code := 201
		if duplicate {
			code = 200
		}
		rt.JSON(w, code, result)
	})
	mux.HandleFunc("GET /internal/notifications/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if !transactionIDPattern.MatchString(id) {
			rt.JSON(w, 400, map[string]string{"error": "invalid transaction ID"})
			return
		}
		var deliveredAt time.Time
		err := pool.QueryRow(r.Context(), `SELECT created_at FROM notification.deliveries WHERE transaction_id=$1 AND status='DELIVERED'`, id).Scan(&deliveredAt)
		if errors.Is(err, pgx.ErrNoRows) {
			rt.JSON(w, 404, map[string]string{"error": "notification receipt not found"})
			return
		}
		if err != nil {
			log.Error("read notification receipt", "error", err)
			rt.JSON(w, 503, map[string]string{"error": "notification status unavailable"})
			return
		}
		rt.JSON(w, 200, struct {
			TransactionID string    `json:"transaction_id"`
			Status        string    `json:"status"`
			DeliveredAt   time.Time `json:"delivered_at"`
		}{id, "DELIVERED", deliveredAt})
	})
	return rt.RequireMutationToken(mux, mutationToken, func(r *http.Request) bool {
		return r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/internal/notifications/dead-letters/") && strings.HasSuffix(r.URL.Path, "/replay")
	})
}
