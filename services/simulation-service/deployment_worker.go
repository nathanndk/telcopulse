package simulation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"telcopulse/services/shared/rpc"
	"telcopulse/services/shared/runtime"
)

const syntheticBadVersion = "1.4.0-sim-bad"

type deploymentReport struct {
	EventID      string    `json:"event_id"`
	DeploymentID string    `json:"deployment_id"`
	Service      string    `json:"service"`
	Version      string    `json:"version"`
	CommitSHA    string    `json:"commit_sha"`
	Environment  string    `json:"environment"`
	Deployer     string    `json:"deployer"`
	Status       string    `json:"status"`
	OccurredAt   time.Time `json:"occurred_at"`
}

// DeploymentWorker reliably reports synthetic release/rollback markers to the
// deployment service. A successful report unlocks bad-release fault selection.
func DeploymentWorker(endpoint, token string) (runtime.Worker, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "http" || (u.Hostname() != "deployment-service" && u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost") || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || len(token) < 32 {
		return nil, errors.New("invalid local deployment service configuration")
	}
	client := rpc.New(strings.TrimRight(endpoint, "/"), token)
	return func(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger) {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			attemptCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			err := publishDeployment(attemptCtx, pool, client, log)
			cancel()
			if err != nil && ctx.Err() == nil {
				log.Warn("synthetic deployment report deferred", "error", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}, nil
}

func publishDeployment(ctx context.Context, pool *pgxpool.Pool, client *rpc.Client, log *slog.Logger) error {
	// Expiry is a database deadline; enqueue its rollback even across process restarts.
	_, err := pool.Exec(ctx, `INSERT INTO simulation.deployment_outbox(run_id,status,event_id,occurred_at)
 SELECT id,'Rolled Back','sim-event-'||substring(id from 5)||'-rollback',expires_at
 FROM simulation.runs WHERE scenario='bad-deployment' AND stopped_at IS NULL AND expires_at<=clock_timestamp()
 ON CONFLICT(run_id,status) DO NOTHING`)
	if err != nil {
		return err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		rollbackCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = tx.Rollback(rollbackCtx)
	}()
	var report deploymentReport
	var runID string
	var stoppedAt *time.Time
	var attempts int
	err = tx.QueryRow(ctx, `SELECT o.run_id,o.event_id,r.deployment_id,r.environment,o.status,o.occurred_at,r.stopped_at,o.attempts
 FROM simulation.deployment_outbox o JOIN simulation.runs r ON r.id=o.run_id
 WHERE o.delivered_at IS NULL AND o.next_attempt_at<=clock_timestamp()
 AND (o.status='Completed' OR EXISTS(SELECT 1 FROM simulation.deployment_outbox first WHERE first.run_id=o.run_id AND first.status='Completed' AND first.delivered_at IS NOT NULL))
 ORDER BY o.occurred_at,o.run_id LIMIT 1 FOR UPDATE OF o SKIP LOCKED`,
	).Scan(&runID, &report.EventID, &report.DeploymentID, &report.Environment, &report.Status, &report.OccurredAt, &stoppedAt, &attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return tx.Commit(ctx)
	}
	if err != nil {
		return err
	}
	report.Service = "payment-service"
	report.Version = syntheticBadVersion
	sum := sha256.Sum256([]byte("telcopulse-synthetic-release:" + report.DeploymentID))
	report.CommitSHA = hex.EncodeToString(sum[:])
	report.Deployer = "sim:local"
	if report.Status == "Rolled Back" && stoppedAt == nil {
		report.Deployer = "sim:expiry"
	}
	response, callErr := client.Exchange(ctx, http.MethodPost, "/internal/deployments/events", report, "")
	if callErr == nil && (response.Status == http.StatusOK || response.Status == http.StatusCreated) {
		_, err = tx.Exec(ctx, `UPDATE simulation.deployment_outbox SET delivered_at=clock_timestamp(),attempts=attempts+1 WHERE run_id=$1 AND status=$2`, runID, report.Status)
		if err != nil {
			return err
		}
		if err = tx.Commit(ctx); err != nil {
			return err
		}
		log.Info("synthetic deployment event reported", "run_id", runID, "deployment_id", report.DeploymentID, "status", report.Status, "event_id", report.EventID)
		return nil
	}
	backoff := min(2<<min(attempts, 4), 30)
	_, err = tx.Exec(ctx, `UPDATE simulation.deployment_outbox SET attempts=attempts+1,next_attempt_at=clock_timestamp()+make_interval(secs=>$3) WHERE run_id=$1 AND status=$2`, runID, report.Status, backoff)
	if err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	if callErr != nil {
		return fmt.Errorf("deployment service call: %w", callErr)
	}
	return fmt.Errorf("deployment service returned HTTP %d", response.Status)
}
