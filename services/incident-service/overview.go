package incident

import (
	"context"
	"encoding/json"
	"errors"
)

// Overview is a consistent active-incident snapshot, independent of telemetry health.
type Overview struct {
	Active   int64     `json:"active"`
	Critical int64     `json:"critical"`
	Items    []Summary `json:"items"`
}

// Overview includes detected through monitoring incidents; resolution and postmortem
// are excluded. Priority is severity, then most recently updated, then ID.
func (s Store) Overview(ctx context.Context, environment string) (Overview, error) {
	out := Overview{Items: []Summary{}}
	if environment != "development" && environment != "staging" {
		return out, invalid(errors.New("invalid environment"))
	}
	var items []byte
	err := s.Pool.QueryRow(ctx, `WITH active AS (
 SELECT id,updated_at,document FROM incident.records WHERE environment=$1 AND state NOT IN ('Resolved','Postmortem')
 ) SELECT count(*),count(*) FILTER(WHERE document->>'severity' IN ('SEV-1','SEV-2')),
 COALESCE((SELECT jsonb_agg(document ORDER BY document->>'severity',updated_at DESC,id DESC) FROM
 (SELECT id,updated_at,document - '{impact,root_cause,mitigation,resolution,postmortem_notes,evidence,action_items}'::text[] AS document FROM active ORDER BY document->>'severity',updated_at DESC,id DESC LIMIT 5) recent),'[]'::jsonb) FROM active`, environment).Scan(&out.Active, &out.Critical, &items)
	if err != nil {
		return out, err
	}
	err = json.Unmarshal(items, &out.Items)
	return out, err
}
