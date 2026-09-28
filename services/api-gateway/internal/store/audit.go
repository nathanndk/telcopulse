package store

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
)

var ErrInvalidAuditFilter = errors.New("invalid audit filter")

type AuditFilter struct {
	Environment string
	Source      string
	Actor       string
	Action      string
	Resource    string
	Cursor      string
	Limit       int
}

type AuditChange struct {
	Field  string `json:"field"`
	Before string `json:"before,omitempty"`
	After  string `json:"after,omitempty"`
}

type AuditEvent struct {
	ID          string        `json:"id"`
	Source      string        `json:"source"`
	Environment string        `json:"environment"`
	ResourceID  string        `json:"resource_id"`
	Actor       string        `json:"actor"`
	Action      string        `json:"action"`
	Note        string        `json:"note"`
	At          time.Time     `json:"at"`
	Changes     []AuditChange `json:"changes"`
}

type AuditPage struct {
	Items []AuditEvent `json:"items"`
	More  bool         `json:"more"`
	Next  string       `json:"next,omitempty"`
}

type auditCursor struct {
	Environment string    `json:"environment"`
	Source      string    `json:"source"`
	Actor       string    `json:"actor"`
	Action      string    `json:"action"`
	Resource    string    `json:"resource"`
	At          time.Time `json:"at"`
	ID          string    `json:"id"`
}

func auditBoundary(f AuditFilter) (auditCursor, error) {
	var boundary auditCursor
	if !slices.Contains([]string{"development", "staging"}, f.Environment) ||
		(f.Source != "" && !slices.Contains([]string{"incident", "simulation", "deployment"}, f.Source)) ||
		len(f.Actor) > 100 || len(f.Action) > 80 || len(f.Resource) > 120 ||
		f.Limit < 1 || f.Limit > 100 || len(f.Cursor) > 1024 {
		return boundary, ErrInvalidAuditFilter
	}
	if f.Cursor == "" {
		return boundary, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(f.Cursor)
	if err != nil || json.Unmarshal(raw, &boundary) != nil || boundary.At.IsZero() ||
		len(boundary.ID) < 3 || len(boundary.ID) > 300 ||
		boundary.Environment != f.Environment || boundary.Source != f.Source ||
		boundary.Actor != f.Actor || boundary.Action != f.Action || boundary.Resource != f.Resource {
		return auditCursor{}, ErrInvalidAuditFilter
	}
	return boundary, nil
}

// AuditEvents is a read-only cross-domain operations projection. It deliberately
// excludes raw transaction/customer details and authentication security events.
func (s *Store) AuditEvents(ctx context.Context, f AuditFilter) (AuditPage, error) {
	out := AuditPage{Items: []AuditEvent{}}
	boundary, err := auditBoundary(f)
	if err != nil {
		return out, err
	}
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return out, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	out, err = auditEventsTx(ctx, tx, f, boundary)
	if err != nil {
		return out, err
	}
	return out, tx.Commit(ctx)
}

func auditEventsTx(ctx context.Context, tx pgx.Tx, f AuditFilter, boundary auditCursor) (AuditPage, error) {
	out := AuditPage{Items: []AuditEvent{}}
	rows, err := tx.Query(ctx, `WITH events AS (
 SELECT 'incident'::text AS source, ('incident:'||a.incident_id||':'||a.version::text) AS id,
 r.environment, a.incident_id AS resource_id, COALESCE(a.entry->>'actor','') AS actor,
	 COALESCE(a.entry->>'action','updated') AS action, ''::text AS note,
 (a.entry->>'at')::timestamptz AS at, a.entry->'before' AS before_doc, a.entry->'after' AS after_doc
 FROM incident.audit a JOIN incident.records r ON r.id=a.incident_id WHERE r.environment=$1
 UNION ALL
 SELECT 'simulation', ('simulation:'||a.sequence::text), r.environment, a.run_id,
	 a.actor, a.action, ''::text, a.at, NULL::jsonb, NULL::jsonb
 FROM simulation.audit a JOIN simulation.runs r ON r.id=a.run_id WHERE r.environment=$1
 UNION ALL
 SELECT 'deployment', ('deployment:'||e.event_id), r.environment, e.deployment_id,
 e.actor, e.status, ''::text, e.recorded_at, NULL::jsonb, NULL::jsonb
 FROM deployment.events e JOIN deployment.records r ON r.id=e.deployment_id WHERE r.environment=$1
)
SELECT id,source,environment,resource_id,actor,action,note,at,before_doc,after_doc
FROM events WHERE ($2='' OR source=$2) AND ($3='' OR position(lower($3) in lower(actor))>0)
 AND ($4='' OR position(lower($4) in lower(action))>0) AND ($5='' OR resource_id=$5)
 AND ($6='' OR (at,id)<($7,$6))
ORDER BY at DESC,id DESC LIMIT $8`, f.Environment, f.Source, f.Actor, f.Action, f.Resource, boundary.ID, boundary.At, f.Limit+1)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var event AuditEvent
		var before, after []byte
		if err = rows.Scan(&event.ID, &event.Source, &event.Environment, &event.ResourceID, &event.Actor, &event.Action, &event.Note, &event.At, &before, &after); err != nil {
			return out, err
		}
		event.Changes, err = safeAuditChanges(before, after)
		if err != nil {
			return out, err
		}
		out.Items = append(out.Items, event)
	}
	if err = rows.Err(); err != nil {
		return out, err
	}
	if len(out.Items) > f.Limit {
		out.More = true
		out.Items = out.Items[:f.Limit]
		last := out.Items[len(out.Items)-1]
		raw, marshalErr := json.Marshal(auditCursor{Environment: f.Environment, Source: f.Source, Actor: f.Actor, Action: f.Action, Resource: f.Resource, At: last.At, ID: last.ID})
		if marshalErr != nil {
			return out, fmt.Errorf("encode audit cursor: %w", marshalErr)
		}
		out.Next = base64.RawURLEncoding.EncodeToString(raw)
	}
	return out, nil
}

// Only approved scalar fields and collection-change markers leave the audit
// service. Full snapshots remain on the source incident detail route.
func safeAuditChanges(beforeRaw, afterRaw []byte) ([]AuditChange, error) {
	changes := []AuditChange{}
	if len(beforeRaw) == 0 || bytes.Equal(bytes.TrimSpace(beforeRaw), []byte("null")) {
		return changes, nil
	}
	var before, after map[string]json.RawMessage
	if err := json.Unmarshal(beforeRaw, &before); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(afterRaw, &after); err != nil {
		return nil, err
	}
	for _, field := range []string{"state", "severity", "owner", "owning_team", "root_cause", "mitigation", "resolution", "impact", "recovery_validation", "evidence", "action_items", "postmortem_notes"} {
		if bytes.Equal(before[field], after[field]) {
			continue
		}
		change := AuditChange{Field: field}
		if slices.Contains([]string{"state", "severity", "owner", "owning_team"}, field) {
			_ = json.Unmarshal(before[field], &change.Before)
			_ = json.Unmarshal(after[field], &change.After)
		}
		changes = append(changes, change)
	}
	return changes, nil
}
