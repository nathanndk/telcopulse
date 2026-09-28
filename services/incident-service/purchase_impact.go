package incident

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
)

var transactionIDPattern = regexp.MustCompile(`^TXN-[a-f0-9]{24}$`)

func isPurchasePath(service string) bool {
	return slices.Contains([]string{"api-gateway", "subscriber-service", "package-service", "payment-service"}, service)
}

// PurchaseImpact is an observed, environment-scoped purchase cohort. It is not
// an estimate of all customers affected or proof that the incident caused a failure.
type PurchaseImpact struct {
	Applicable      bool             `json:"applicable"`
	Scope           string           `json:"scope"`
	WindowStart     time.Time        `json:"window_start"`
	WindowEnd       time.Time        `json:"window_end"`
	Total           int64            `json:"total"`
	Success         int64            `json:"success"`
	Failed          int64            `json:"failed"`
	FailedCustomers int64            `json:"failed_customers"`
	Items           []FailedPurchase `json:"items"`
	More            bool             `json:"more"`
	Next            string           `json:"next,omitempty"`
}

type FailedPurchase struct {
	ID         string    `json:"id"`
	TraceID    string    `json:"trace_id"`
	CustomerID string    `json:"customer_id"`
	ErrorCode  string    `json:"error_code"`
	CreatedAt  time.Time `json:"created_at"`
}

type purchaseCursor struct {
	IncidentID string
	Window     string
	At         time.Time
	ID         string
}

// PurchaseImpact uses the persisted incident envelope and a fixed purchase-path
// allowlist. Other services cannot be attributed to this workflow from this table.
func (s Store) PurchaseImpact(ctx context.Context, i Incident, window, encoded string) (PurchaseImpact, error) {
	out := PurchaseImpact{Applicable: false, Scope: "purchase_path_environment", Items: []FailedPurchase{}}
	start, end, err := logWindow(i, window, time.Now().UTC())
	if err != nil {
		return out, err
	}
	if window == "" {
		window = "24h"
	}
	out.WindowStart, out.WindowEnd = start, end
	if !isPurchasePath(i.Service) {
		if encoded != "" {
			return out, invalid(errors.New("cursor is not valid for this incident service"))
		}
		return out, nil
	}
	out.Applicable = true
	var boundary purchaseCursor
	if len(encoded) > 512 {
		return out, invalid(errors.New("invalid purchase cursor"))
	}
	if encoded != "" {
		raw, decodeErr := base64.RawURLEncoding.DecodeString(encoded)
		if decodeErr != nil || json.Unmarshal(raw, &boundary) != nil || boundary.IncidentID != i.ID || boundary.Window != window || boundary.At.Before(start) || !boundary.At.Before(end) || !transactionIDPattern.MatchString(boundary.ID) {
			return out, invalid(errors.New("invalid purchase cursor"))
		}
	}
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return out, err
	}
	defer rollback(tx)
	err = tx.QueryRow(ctx, `SELECT count(*),count(*) FILTER (WHERE status='SUCCESS'),count(*) FILTER (WHERE status='FAILED'),count(DISTINCT customer_id) FILTER (WHERE status='FAILED')
  FROM transactions WHERE environment=$1 AND created_at>=$2 AND created_at<$3`, i.Environment, start, end).Scan(&out.Total, &out.Success, &out.Failed, &out.FailedCustomers)
	if err != nil {
		return out, err
	}
	rows, err := tx.Query(ctx, `SELECT id,trace_id,customer_id,COALESCE(left(result->>'error_code',100),''),created_at
  FROM transactions WHERE environment=$1 AND status='FAILED' AND created_at>=$2 AND created_at<$3
   AND ($4='' OR (created_at,id)<($5,$4)) ORDER BY created_at DESC,id DESC LIMIT 21`, i.Environment, start, end, boundary.ID, boundary.At)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var item FailedPurchase
		if err = rows.Scan(&item.ID, &item.TraceID, &item.CustomerID, &item.ErrorCode, &item.CreatedAt); err != nil {
			rows.Close()
			return out, err
		}
		out.Items = append(out.Items, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	if len(out.Items) > 20 {
		out.More = true
		out.Items = out.Items[:20]
		last := out.Items[19]
		raw, marshalErr := json.Marshal(purchaseCursor{IncidentID: i.ID, Window: window, At: last.CreatedAt, ID: last.ID})
		if marshalErr != nil {
			return out, marshalErr
		}
		out.Next = base64.RawURLEncoding.EncodeToString(raw)
	}
	if err = tx.Commit(ctx); err != nil {
		return out, err
	}
	return out, nil
}
