package incident

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

const recoveryWindow = 5 * time.Minute
const recoveryMinimumOutcomes = 5

// RecoveryAssessment is a live synthetic purchase sample, not an attestation
// that all customers or the affected service have recovered.
type RecoveryAssessment struct {
	Applicable        bool       `json:"applicable"`
	Scope             string     `json:"scope"`
	Status            string     `json:"status"`
	EvaluatedAt       time.Time  `json:"evaluated_at"`
	MonitoringAt      *time.Time `json:"monitoring_at,omitempty"`
	EarlierStart      time.Time  `json:"earlier_start"`
	SplitAt           time.Time  `json:"split_at"`
	WindowEnd         time.Time  `json:"window_end"`
	EarlierTotal      int64      `json:"earlier_total"`
	EarlierSuccess    int64      `json:"earlier_success"`
	EarlierFailed     int64      `json:"earlier_failed"`
	RecentTotal       int64      `json:"recent_total"`
	RecentSuccess     int64      `json:"recent_success"`
	RecentFailed      int64      `json:"recent_failed"`
	MinimumOutcomes   int        `json:"minimum_outcomes"`
	TargetSuccessRate float64    `json:"target_success_rate"`
}

func assessmentStatus(a RecoveryAssessment) string {
	if !a.Applicable {
		return "not_applicable"
	}
	if a.MonitoringAt == nil {
		return "not_monitoring"
	}
	if a.MonitoringAt.After(a.EarlierStart) {
		return "collecting"
	}
	if a.EarlierTotal < recoveryMinimumOutcomes || a.RecentTotal < recoveryMinimumOutcomes {
		return "insufficient_traffic"
	}
	if float64(a.EarlierSuccess)/float64(a.EarlierTotal) < a.TargetSuccessRate || float64(a.RecentSuccess)/float64(a.RecentTotal) < a.TargetSuccessRate {
		return "still_failing"
	}
	return "meets_target"
}

// AssessRecovery counts terminal, persisted synthetic purchase outcomes in two
// adjacent five-minute windows. One repeatable-read snapshot keeps the state,
// monitoring start and counts aligned despite concurrent purchase completions.
func (s Store) AssessRecovery(ctx context.Context, incidentID string, now time.Time) (RecoveryAssessment, error) {
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return RecoveryAssessment{}, err
	}
	defer rollback(tx)
	a, err := assessRecoveryTx(ctx, tx, incidentID, now)
	if err != nil {
		return a, err
	}
	return a, tx.Commit(ctx)
}

func assessRecoveryTx(ctx context.Context, tx pgx.Tx, incidentID string, now time.Time) (RecoveryAssessment, error) {
	a := RecoveryAssessment{Scope: "purchase_path_environment", EvaluatedAt: now.UTC(), EarlierStart: now.UTC().Add(-2 * recoveryWindow), SplitAt: now.UTC().Add(-recoveryWindow), WindowEnd: now.UTC(), MinimumOutcomes: recoveryMinimumOutcomes, TargetSuccessRate: 0.999}
	var service, environment string
	var state State
	err := tx.QueryRow(ctx, `SELECT document->>'service',environment,state FROM incident.records WHERE id=$1`, incidentID).Scan(&service, &environment, &state)
	if errors.Is(err, pgx.ErrNoRows) {
		return a, ErrNotFound
	}
	if err != nil {
		return a, err
	}
	a.Applicable = isPurchasePath(service)
	if a.Applicable && state == Monitoring {
		var monitoring sql.NullTime
		err = tx.QueryRow(ctx, `SELECT max((entry->>'at')::timestamptz) FROM incident.audit
 WHERE incident_id=$1 AND entry->'after'->>'state'='Monitoring'
 AND entry->'before'->>'state' IS DISTINCT FROM 'Monitoring'`, incidentID).Scan(&monitoring)
		if err != nil {
			return a, err
		}
		if monitoring.Valid {
			a.MonitoringAt = &monitoring.Time
		}
		if a.MonitoringAt != nil {
			err = tx.QueryRow(ctx, `SELECT
 count(*) FILTER (WHERE p.updated_at<$3),
 count(*) FILTER (WHERE p.updated_at<$3 AND t.status='SUCCESS'),
 count(*) FILTER (WHERE p.updated_at<$3 AND t.status='FAILED'),
 count(*) FILTER (WHERE p.updated_at>=$3),
 count(*) FILTER (WHERE p.updated_at>=$3 AND t.status='SUCCESS'),
 count(*) FILTER (WHERE p.updated_at>=$3 AND t.status='FAILED')
 FROM purchase_workflows p JOIN transactions t ON t.id=p.transaction_id
 WHERE p.state IN ('SUCCESS','FAILED') AND p.updated_at>=$2 AND p.updated_at<$4 AND t.environment=$1`, environment, a.EarlierStart, a.SplitAt, a.WindowEnd).Scan(&a.EarlierTotal, &a.EarlierSuccess, &a.EarlierFailed, &a.RecentTotal, &a.RecentSuccess, &a.RecentFailed)
			if err != nil {
				return a, err
			}
		}
	}
	a.Status = assessmentStatus(a)
	return a, nil
}
