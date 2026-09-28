package incident

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Operations summarizes incident response over one measured environment/window.
type Operations struct {
	WindowStart       time.Time `json:"window_start"`
	WindowEnd         time.Time `json:"window_end"`
	IncidentCount     int64     `json:"incident_count"`
	SEV1Count         int64     `json:"sev1_count"`
	AcknowledgedCount int64     `json:"acknowledged_count"`
	MTTASeconds       *float64  `json:"mtta_seconds"`
	ResolvedCount     int64     `json:"resolved_count"`
	MTTRSeconds       *float64  `json:"mttr_seconds"`
	EscalationEvents  int64     `json:"escalation_events"`
	RepeatCount       int64     `json:"repeat_count"`
}

// Operations uses detection cohorts for response times and audit event time for
// escalations. Repeats share a service and normalized root cause within 30 days.
func (s Store) Operations(ctx context.Context, environment string) (Operations, error) {
	var out Operations
	if environment != "development" && environment != "staging" {
		return out, invalid(errors.New("invalid environment"))
	}
	var mtta, mttr sql.NullFloat64
	err := s.Pool.QueryRow(ctx, `WITH bounds AS (
 SELECT now()-interval '30 days' AS start_at, now() AS end_at
), extracted AS (
 SELECT r.id,r.document->>'service' AS service,r.document->>'severity' AS severity,
 lower(btrim(COALESCE(r.document->>'root_cause',''))) AS root_cause,
 (r.document->>'detected_at')::timestamptz AS detected_at,
 (r.document->>'acknowledged_at')::timestamptz AS acknowledged_at,
 (r.document->>'resolved_at')::timestamptz AS resolved_at
 FROM incident.records r WHERE r.environment=$1
), cohort AS (
 SELECT e.*,row_number() OVER (PARTITION BY e.service,e.root_cause ORDER BY e.detected_at,e.id) AS occurrence
 FROM extracted e CROSS JOIN bounds b WHERE e.detected_at>=b.start_at AND e.detected_at<b.end_at
), totals AS (
 SELECT count(*) AS incidents,count(*) FILTER (WHERE severity='SEV-1') AS sev1,
 count(*) FILTER (WHERE acknowledged_at>=detected_at) AS acknowledged,
 avg(extract(epoch FROM acknowledged_at-detected_at)::double precision) FILTER (WHERE acknowledged_at>=detected_at) AS mtta,
 count(*) FILTER (WHERE resolved_at>=detected_at) AS resolved,
 avg(extract(epoch FROM resolved_at-detected_at)::double precision) FILTER (WHERE resolved_at>=detected_at) AS mttr,
 count(*) FILTER (WHERE length(root_cause)>=5 AND occurrence>1) AS repeats FROM cohort
), escalations AS (
 SELECT count(*) AS events FROM incident.audit a JOIN incident.records r ON r.id=a.incident_id
 CROSS JOIN bounds b WHERE r.environment=$1 AND a.entry->>'action'='escalated'
 AND (a.entry->>'at')::timestamptz>=b.start_at AND (a.entry->>'at')::timestamptz<b.end_at
)
SELECT b.start_at,b.end_at,t.incidents,t.sev1,t.acknowledged,t.mtta,t.resolved,t.mttr,e.events,t.repeats
FROM bounds b CROSS JOIN totals t CROSS JOIN escalations e`, environment).Scan(
		&out.WindowStart, &out.WindowEnd, &out.IncidentCount, &out.SEV1Count,
		&out.AcknowledgedCount, &mtta, &out.ResolvedCount, &mttr,
		&out.EscalationEvents, &out.RepeatCount,
	)
	if err != nil {
		return out, err
	}
	if mtta.Valid {
		out.MTTASeconds = &mtta.Float64
	}
	if mttr.Valid {
		out.MTTRSeconds = &mttr.Float64
	}
	return out, nil
}
