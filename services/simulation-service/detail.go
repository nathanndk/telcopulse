package simulation

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"time"
)

// AuditEntry records the operator's immutable start or stop command.
type AuditEntry struct {
	Action string    `json:"action"`
	Actor  string    `json:"actor"`
	Note   string    `json:"note"`
	At     time.Time `json:"at"`
}

// TransactionDecision links a sampled decision to its transaction investigation.
type TransactionDecision struct {
	TransactionID string    `json:"transaction_id"`
	Inject        bool      `json:"inject"`
	At            time.Time `json:"at"`
}

// Detail separates selected failure decisions from confirmed business impact.
type Detail struct {
	Run              Run                   `json:"run"`
	DeploymentReport string                `json:"deployment_report"`
	Audit            []AuditEntry          `json:"audit"`
	Observed         int64                 `json:"observed"`
	Selected         int64                 `json:"selected"`
	Decisions        []TransactionDecision `json:"decisions"`
	More             bool                  `json:"more"`
	Next             string                `json:"next,omitempty"`
}
type decisionCursor struct {
	RunID         string `json:"run_id"`
	TransactionID string `json:"transaction_id"`
}

// Get reads one consistent snapshot; subsequent pages reflect current database state.
func (s Store) Get(ctx context.Context, id, cursor string) (Detail, error) {
	var out Detail
	if !runID.MatchString(id) || len(cursor) > 512 {
		return out, ErrInvalid
	}
	after := ""
	if cursor != "" {
		payload, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil {
			return out, ErrInvalid
		}
		var decoded decisionCursor
		if json.Unmarshal(payload, &decoded) != nil || decoded.RunID != id || !transactionID.MatchString(decoded.TransactionID) {
			return out, ErrInvalid
		}
		after = decoded.TransactionID
	}
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return out, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	out.Run, err = scan(tx.QueryRow(ctx, "SELECT "+columns+" FROM simulation.runs WHERE id=$1", id))
	if err != nil {
		return out, err
	}
	if out.Run.DeploymentID != "" {
		if err = tx.QueryRow(ctx, `SELECT CASE
 WHEN EXISTS(SELECT 1 FROM simulation.deployment_outbox WHERE run_id=$1 AND status='Rolled Back' AND delivered_at IS NOT NULL) THEN 'Rolled Back'
 WHEN EXISTS(SELECT 1 FROM simulation.deployment_outbox WHERE run_id=$1 AND status='Rolled Back') THEN 'Rollback queued'
 WHEN EXISTS(SELECT 1 FROM simulation.deployment_outbox WHERE run_id=$1 AND status='Completed' AND delivered_at IS NOT NULL) THEN 'Completed'
 ELSE 'Reporting queued' END`, id).Scan(&out.DeploymentReport); err != nil {
			return out, err
		}
	}
	out.Audit = []AuditEntry{}
	out.Decisions = []TransactionDecision{}
	rows, err := tx.Query(ctx, "SELECT action,actor,note,at FROM simulation.audit WHERE run_id=$1 ORDER BY sequence", id)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var entry AuditEntry
		if err = rows.Scan(&entry.Action, &entry.Actor, &entry.Note, &entry.At); err != nil {
			rows.Close()
			return out, err
		}
		out.Audit = append(out.Audit, entry)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	if err = tx.QueryRow(ctx, "SELECT count(*),count(*) FILTER(WHERE inject) FROM simulation.decisions WHERE run_id=$1", id).Scan(&out.Observed, &out.Selected); err != nil {
		return out, err
	}
	rows, err = tx.Query(ctx, "SELECT transaction_id,inject,at FROM simulation.decisions WHERE run_id=$1 AND ($2='' OR transaction_id<$2) ORDER BY transaction_id DESC LIMIT 21", id, after)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var d TransactionDecision
		if err = rows.Scan(&d.TransactionID, &d.Inject, &d.At); err != nil {
			rows.Close()
			return out, err
		}
		out.Decisions = append(out.Decisions, d)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	if len(out.Decisions) > 20 {
		out.More = true
		out.Decisions = out.Decisions[:20]
		payload, e := json.Marshal(decisionCursor{RunID: id, TransactionID: out.Decisions[19].TransactionID})
		if e != nil {
			return out, e
		}
		out.Next = base64.RawURLEncoding.EncodeToString(payload)
	}
	if err = tx.Commit(ctx); err != nil {
		return out, err
	}
	return out, nil
}
