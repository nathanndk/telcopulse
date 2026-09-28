package deployment

import (
	"context"
	"time"
)

// Operations measures the result of deployments reaching their first terminal
// result in the last 30 days; a later rollback counts as a failed deployment.
type Operations struct {
	WindowStart        time.Time `json:"window_start"`
	WindowEnd          time.Time `json:"window_end"`
	Evaluated          int64     `json:"evaluated"`
	Failed             int64     `json:"failed"`
	FailureRatePercent *float64  `json:"failure_rate_percent"`
}

func (s Store) Operations(ctx context.Context, environment string) (Operations, error) {
	var out Operations
	if environment != "development" && environment != "staging" && environment != "production" {
		return out, ErrInvalid
	}
	err := s.Pool.QueryRow(ctx, `WITH bounds AS (
 SELECT now()-interval '30 days' AS start_at,now() AS end_at
), cohort AS (
 SELECT r.id,r.status FROM deployment.records r CROSS JOIN bounds b
 WHERE r.environment=$1 AND EXISTS (
 SELECT 1 FROM deployment.events e WHERE e.deployment_id=r.id
 AND e.status IN ('Completed','Failed') AND e.occurred_at>=b.start_at AND e.occurred_at<b.end_at)
)
SELECT b.start_at,b.end_at,count(c.id),count(c.id) FILTER (WHERE c.status IN ('Failed','Rolled Back'))
FROM bounds b LEFT JOIN cohort c ON true GROUP BY b.start_at,b.end_at`, environment).Scan(
		&out.WindowStart, &out.WindowEnd, &out.Evaluated, &out.Failed,
	)
	if err != nil {
		return out, err
	}
	if out.Evaluated > 0 {
		rate := 100 * float64(out.Failed) / float64(out.Evaluated)
		out.FailureRatePercent = &rate
	}
	return out, nil
}
