package incident

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"
)

// ActionFilter scopes the follow-up register; the incident document remains
// authoritative and action edits still use the versioned incident command.
type ActionFilter struct {
	Environment string
	Status      string
	Priority    string
	Owner       string
	Search      string
	Limit       int
	Cursor      string
}

type ActionRow struct {
	IncidentID        string     `json:"incident_id"`
	IncidentTitle     string     `json:"incident_title"`
	IncidentState     State      `json:"incident_state"`
	IncidentService   string     `json:"incident_service"`
	IncidentSeverity  string     `json:"incident_severity"`
	IncidentUpdatedAt time.Time  `json:"incident_updated_at"`
	Position          int64      `json:"position"`
	Title             string     `json:"title"`
	Owner             string     `json:"owner"`
	Priority          string     `json:"priority"`
	Status            string     `json:"status"`
	DueAt             *time.Time `json:"due_at"`
}

type ActionPage struct {
	Items []ActionRow `json:"items"`
	More  bool        `json:"more"`
	Next  string      `json:"next,omitempty"`
}

type actionCursor struct {
	Environment string `json:"environment"`
	Status      string `json:"status"`
	Priority    string `json:"priority"`
	Owner       string `json:"owner"`
	Search      string `json:"search"`
	Offset      int    `json:"offset"`
}

// Actions lists normalized legacy and current follow-ups without mutating
// historical incident JSON. Offset pages are deterministic for a static result
// set; a concurrent incident edit can shift later pages.
func (s Store) Actions(ctx context.Context, filter ActionFilter) (ActionPage, error) {
	out := ActionPage{Items: []ActionRow{}}
	filter.Owner = strings.TrimSpace(filter.Owner)
	filter.Search = strings.TrimSpace(filter.Search)
	if !slices.Contains([]string{"development", "staging"}, filter.Environment) ||
		!slices.Contains([]string{"active", "all", "Open", "In Progress", "Blocked", "Completed"}, filter.Status) ||
		(filter.Priority != "" && !slices.Contains([]string{"P1", "P2", "P3"}, filter.Priority)) ||
		len(filter.Owner) > 100 || len(filter.Search) > 100 || filter.Limit < 1 || filter.Limit > 100 {
		return out, invalid(errors.New("invalid action filters"))
	}
	offset := 0
	if filter.Cursor != "" {
		if len(filter.Cursor) > 1024 {
			return out, invalid(errors.New("invalid action cursor"))
		}
		var cursor actionCursor
		raw, err := base64.RawURLEncoding.DecodeString(filter.Cursor)
		if err != nil || json.Unmarshal(raw, &cursor) != nil || cursor.Environment != filter.Environment ||
			cursor.Status != filter.Status || cursor.Priority != filter.Priority || cursor.Owner != filter.Owner ||
			cursor.Search != filter.Search || cursor.Offset < 0 || cursor.Offset > 10000000 {
			return out, invalid(errors.New("invalid action cursor"))
		}
		offset = cursor.Offset
	}
	rows, err := s.Pool.Query(ctx, `WITH flattened AS (
	 SELECT r.id AS incident_id, r.document->>'title' AS incident_title, r.state AS incident_state,
	        r.document->>'service' AS incident_service, r.document->>'severity' AS incident_severity,
	        r.updated_at AS incident_updated_at, a.ordinality AS position,
	        a.item->>'title' AS title, a.item->>'owner' AS owner,
	        COALESCE(NULLIF(a.item->>'priority',''),'P2') AS priority,
	        COALESCE(NULLIF(a.item->>'status',''),CASE WHEN a.item->>'done'='true' THEN 'Completed' ELSE 'Open' END) AS status,
	        NULLIF(a.item->>'due_at','')::timestamptz AS due_at
	 FROM incident.records r
	 CROSS JOIN LATERAL jsonb_array_elements(CASE WHEN jsonb_typeof(r.document->'action_items')='array'
	     THEN r.document->'action_items' ELSE '[]'::jsonb END) WITH ORDINALITY AS a(item,ordinality)
	 WHERE r.environment=$1
	)
	SELECT incident_id,incident_title,incident_state,incident_service,incident_severity,incident_updated_at,
	       position,title,owner,priority,status,due_at
	FROM flattened
	WHERE ($2='all' OR ($2='active' AND status<>'Completed') OR status=$2)
	  AND ($3='' OR priority=$3)
	  AND ($4='' OR strpos(lower(owner),lower($4))>0)
	  AND ($5='' OR strpos(lower(title),lower($5))>0 OR strpos(lower(incident_title),lower($5))>0 OR strpos(lower(incident_id),lower($5))>0)
	ORDER BY (status='Completed') ASC,due_at ASC NULLS LAST,
	         CASE priority WHEN 'P1' THEN 1 WHEN 'P2' THEN 2 ELSE 3 END ASC,
	         incident_updated_at DESC,incident_id DESC,position ASC
	LIMIT $6 OFFSET $7`, filter.Environment, filter.Status, filter.Priority, filter.Owner, filter.Search, filter.Limit+1, offset)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var item ActionRow
		if err := rows.Scan(&item.IncidentID, &item.IncidentTitle, &item.IncidentState, &item.IncidentService,
			&item.IncidentSeverity, &item.IncidentUpdatedAt, &item.Position, &item.Title, &item.Owner,
			&item.Priority, &item.Status, &item.DueAt); err != nil {
			return out, err
		}
		out.Items = append(out.Items, item)
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	if len(out.Items) > filter.Limit {
		out.Items = out.Items[:filter.Limit]
		out.More = true
		cursor := actionCursor{Environment: filter.Environment, Status: filter.Status, Priority: filter.Priority,
			Owner: filter.Owner, Search: filter.Search, Offset: offset + filter.Limit}
		encoded, err := json.Marshal(cursor)
		if err != nil {
			return ActionPage{}, err
		}
		out.Next = base64.RawURLEncoding.EncodeToString(encoded)
	}
	return out, nil
}
