package httpapi

import (
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"time"

	"telcopulse/services/api-gateway/internal/store"
	"telcopulse/services/shared/rpc"
)

var notificationTransactionID = regexp.MustCompile(`^TXN-[a-f0-9]{24}$`)

type notificationStatus struct {
	TransactionID string     `json:"transaction_id"`
	Status        string     `json:"status"`
	DeliveredAt   *time.Time `json:"delivered_at"`
}

func (s *Server) notificationStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !notificationTransactionID.MatchString(id) {
		s.write(w, 400, map[string]string{"error": "invalid transaction ID"})
		return
	}
	transaction, err := s.Repo.Transaction(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		s.write(w, 404, map[string]string{"error": "resource not found"})
		return
	}
	if err != nil {
		s.Log.Error("read transaction for notification", "error", err)
		s.write(w, 503, map[string]string{"error": "notification status unavailable"})
		return
	}
	if transaction.Status == "PROCESSING" {
		s.write(w, 200, notificationStatus{TransactionID: id, Status: "PROCESSING"})
		return
	}
	if s.Notifications == nil {
		s.write(w, 503, map[string]string{"error": "notification status unavailable"})
		return
	}
	var receipt struct {
		TransactionID string    `json:"transaction_id"`
		Status        string    `json:"status"`
		DeliveredAt   time.Time `json:"delivered_at"`
	}
	err = s.Notifications.Call(r.Context(), http.MethodGet, "/internal/notifications/"+id, nil, &receipt, transaction.TraceID, id)
	if err != nil {
		var remote *rpc.Error
		if errors.As(err, &remote) && remote.Status == 404 {
			s.write(w, 200, notificationStatus{TransactionID: id, Status: "AWAITING_DELIVERY"})
			return
		}
		s.Log.Warn("read notification receipt", "transaction_id", id, "error", err)
		s.write(w, 503, map[string]string{"error": "notification status unavailable"})
		return
	}
	if receipt.TransactionID != id || receipt.Status != "DELIVERED" || receipt.DeliveredAt.IsZero() {
		s.Log.Warn("invalid notification receipt", "transaction_id", id)
		s.write(w, 503, map[string]string{"error": "notification status unavailable"})
		return
	}
	s.write(w, 200, notificationStatus{TransactionID: id, Status: "DELIVERED", DeliveredAt: &receipt.DeliveredAt})
}

func (s *Server) deadLetters(w http.ResponseWriter, r *http.Request) {
	if s.Notifications == nil {
		s.write(w, 503, map[string]string{"error": "dead-letter register unavailable"})
		return
	}
	query := url.Values{}
	for key, values := range r.URL.Query() {
		if key != "reason" && key != "limit" && key != "cursor" {
			s.write(w, 400, map[string]string{"error": "unknown dead-letter filter"})
			return
		}
		if len(values) != 1 {
			s.write(w, 400, map[string]string{"error": "duplicate dead-letter filter"})
			return
		}
		query.Set(key, values[0])
	}
	path := "/internal/notifications/dead-letters"
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	result, err := s.Notifications.Exchange(r.Context(), http.MethodGet, path, nil, "")
	if err != nil {
		s.Log.Warn("read dead-letter register", "error", err)
		s.write(w, 503, map[string]string{"error": "dead-letter register unavailable"})
		return
	}
	if result.Status != 200 && result.Status != 400 && result.Status != 422 {
		s.write(w, 503, map[string]string{"error": "dead-letter register unavailable"})
		return
	}
	s.write(w, result.Status, result.Body)
}

func (s *Server) replayDeadLetter(w http.ResponseWriter, r *http.Request) {
	if s.Notifications == nil {
		s.write(w, 503, map[string]string{"error": "dead-letter replay unavailable"})
		return
	}
	key := r.Header.Get("Idempotency-Key")
	if !keyPattern.MatchString(key) {
		s.write(w, 400, map[string]string{"error": "invalid replay key"})
		return
	}
	partition, err := strconv.ParseInt(r.PathValue("partition"), 10, 32)
	if err != nil || partition < 0 {
		s.write(w, 400, map[string]string{"error": "invalid source partition"})
		return
	}
	offset, err := strconv.ParseInt(r.PathValue("offset"), 10, 64)
	if err != nil || offset < 0 {
		s.write(w, 400, map[string]string{"error": "invalid source offset"})
		return
	}
	if r.ContentLength != 0 {
		s.write(w, 400, map[string]string{"error": "replay takes no payload"})
		return
	}
	actor, role := "", ""
	if s.RequireAuth {
		operator := operatorFrom(r.Context())
		actor, role = "operator:"+operator.ID, operator.Role
	}
	path := "/internal/notifications/dead-letters/" + strconv.FormatInt(partition, 10) + "/" + strconv.FormatInt(offset, 10) + "/replay"
	result, err := s.Notifications.ExchangeAs(r.Context(), http.MethodPost, path, nil, key, actor, role)
	if err != nil {
		s.Log.Warn("replay dead letter", "error", err)
		s.write(w, 503, map[string]string{"error": "dead-letter replay unavailable"})
		return
	}
	switch result.Status {
	case 200, 201, 400, 403, 404, 409, 422:
		s.write(w, result.Status, result.Body)
	default:
		s.write(w, 503, map[string]string{"error": "dead-letter replay unavailable"})
	}
}

func (s *Server) deadLetterReplayHistory(w http.ResponseWriter, r *http.Request) {
	if s.Notifications == nil {
		s.write(w, 503, map[string]string{"error": "replay history unavailable"})
		return
	}
	for name, values := range r.URL.Query() {
		if (name != "cursor" && name != "limit") || len(values) != 1 {
			s.write(w, 400, map[string]string{"error": "invalid replay history filter"})
			return
		}
	}
	partition, err := strconv.ParseInt(r.PathValue("partition"), 10, 32)
	if err != nil || partition < 0 {
		s.write(w, 400, map[string]string{"error": "invalid source partition"})
		return
	}
	offset, err := strconv.ParseInt(r.PathValue("offset"), 10, 64)
	if err != nil || offset < 0 {
		s.write(w, 400, map[string]string{"error": "invalid source offset"})
		return
	}
	query := url.Values{}
	if cursor := r.URL.Query().Get("cursor"); cursor != "" {
		if len(cursor) > 1024 {
			s.write(w, 422, map[string]string{"error": "invalid replay history cursor"})
			return
		}
		query.Set("cursor", cursor)
	}
	if limit := r.URL.Query().Get("limit"); limit != "" {
		parsed, parseErr := strconv.Atoi(limit)
		if parseErr != nil || parsed < 1 || parsed > 50 {
			s.write(w, 422, map[string]string{"error": "invalid replay history page size"})
			return
		}
		query.Set("limit", limit)
	}
	path := "/internal/notifications/dead-letters/" + strconv.FormatInt(partition, 10) + "/" + strconv.FormatInt(offset, 10) + "/replays"
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	result, err := s.Notifications.Exchange(r.Context(), http.MethodGet, path, nil, "")
	if err != nil {
		s.Log.Warn("read replay history", "error", err)
		s.write(w, 503, map[string]string{"error": "replay history unavailable"})
		return
	}
	switch result.Status {
	case 200, 400, 404, 422:
		s.write(w, result.Status, result.Body)
	default:
		s.write(w, 503, map[string]string{"error": "replay history unavailable"})
	}
}
