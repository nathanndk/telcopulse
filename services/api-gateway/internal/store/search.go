package store

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

var ErrInvalidWorkspaceSearch = errors.New("invalid workspace search")

type WorkspaceHit struct {
	Source     string    `json:"source"`
	ResourceID string    `json:"resource_id"`
	Title      string    `json:"title"`
	Detail     string    `json:"detail"`
	At         time.Time `json:"at"`
}

type WorkspaceResults struct {
	Items []WorkspaceHit `json:"items"`
}

func validateWorkspaceSearch(environment, query string) (string, error) {
	query = strings.TrimSpace(query)
	if (environment != "development" && environment != "staging") ||
		utf8.RuneCountInString(query) < 2 || utf8.RuneCountInString(query) > 80 ||
		strings.ContainsAny(query, "\x00\r\n\t") {
		return "", ErrInvalidWorkspaceSearch
	}
	return query, nil
}

// WorkspaceSearch returns a bounded, environment-scoped navigation projection.
// It never returns transaction result JSON, incident snapshots or simulation reasons.
func (s *Store) WorkspaceSearch(ctx context.Context, environment, query string) (WorkspaceResults, error) {
	out := WorkspaceResults{Items: []WorkspaceHit{}}
	query, err := validateWorkspaceSearch(environment, query)
	if err != nil {
		return out, err
	}
	rows, err := s.Pool.Query(ctx, workspaceSearchSQL, environment, strings.ToLower(query))
	if err != nil {
		return out, err
	}
	defer rows.Close()
	return scanWorkspaceHits(rows)
}

func scanWorkspaceHits(rows pgx.Rows) (WorkspaceResults, error) {
	out := WorkspaceResults{Items: []WorkspaceHit{}}
	for rows.Next() {
		var hit WorkspaceHit
		var rank int
		if err := rows.Scan(&hit.Source, &hit.ResourceID, &hit.Title, &hit.Detail, &hit.At, &rank); err != nil {
			return out, err
		}
		out.Items = append(out.Items, hit)
	}
	return out, rows.Err()
}

const workspaceSearchSQL = `WITH transaction_hits AS (
 SELECT 'transaction'::text AS source,id AS resource_id,id AS title,
  LEFT(status||' · trace '||trace_id,160) AS detail,created_at AS at,
  CASE WHEN lower(id)=$2 OR lower(trace_id)=$2 THEN 2 WHEN position($2 in lower(id))=1 THEN 1 ELSE 0 END AS rank
 FROM operation_transactions WHERE environment=$1
  AND (position($2 in lower(id))>0 OR position($2 in lower(trace_id))>0)
 ORDER BY rank DESC,created_at DESC,id DESC LIMIT 5
), incident_hits AS (
 SELECT 'incident'::text,r.id,LEFT(COALESCE(NULLIF(r.document->>'title',''),r.id),120),
  LEFT(r.id||' · '||COALESCE(r.document->>'service','')||' · '||COALESCE(r.document->>'severity','')||' · '||r.state,160),r.updated_at,
  CASE WHEN lower(r.id)=$2 THEN 2 WHEN position($2 in lower(r.id))=1 THEN 1 ELSE 0 END
 FROM incident.records r WHERE r.environment=$1
  AND (position($2 in lower(r.id))>0 OR position($2 in lower(COALESCE(r.document->>'title','')))>0
   OR position($2 in lower(COALESCE(r.document->>'service','')))>0)
 ORDER BY 6 DESC,r.updated_at DESC,r.id DESC LIMIT 5
), deployment_hits AS (
 SELECT 'deployment'::text,r.id,LEFT(r.service||' '||r.version,120),
  LEFT(r.status||' · '||r.id,160),r.occurred_at,
  CASE WHEN lower(r.id)=$2 THEN 2 WHEN position($2 in lower(r.id))=1 THEN 1 ELSE 0 END
 FROM deployment.records r WHERE r.environment=$1
  AND (position($2 in lower(r.id))>0 OR position($2 in lower(r.service))>0 OR position($2 in lower(r.version))>0)
 ORDER BY 6 DESC,r.occurred_at DESC,r.id DESC LIMIT 5
), simulation_hits AS (
 SELECT 'simulation'::text,r.id,LEFT(r.scenario,120),
  LEFT(r.id||' · '||r.percentage::text||'% · '||CASE WHEN r.stopped_at IS NULL AND r.expires_at>now() THEN 'Active' ELSE 'Stopped or expired' END,160),
  r.started_at,CASE WHEN lower(r.id)=$2 THEN 2 WHEN position($2 in lower(r.id))=1 THEN 1 ELSE 0 END
 FROM simulation.runs r WHERE r.environment=$1
  AND (position($2 in lower(r.id))>0 OR position($2 in lower(r.scenario))>0)
 ORDER BY 6 DESC,r.started_at DESC,r.id DESC LIMIT 5
)
SELECT source,resource_id,title,detail,at,rank FROM (
 SELECT * FROM transaction_hits UNION ALL SELECT * FROM incident_hits
 UNION ALL SELECT * FROM deployment_hits UNION ALL SELECT * FROM simulation_hits
) matches ORDER BY rank DESC,at DESC,source,resource_id DESC LIMIT 20`
