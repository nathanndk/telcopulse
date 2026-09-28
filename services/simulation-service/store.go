// Package simulation owns controlled local failure runs and durable decisions.
package simulation

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"telcopulse/services/shared/domain"
	"telcopulse/services/shared/faults"
)

// ErrConflict rejects a changed command, overlapping run or transaction scope.
var ErrConflict = errors.New("simulation conflict")

// ErrInvalid rejects unbounded or unsupported failure commands.
var ErrInvalid = errors.New("invalid simulation command")

// ErrForbidden rejects an unauthorized operator identity or role.
var ErrForbidden = errors.New("simulation action forbidden")

// Store owns simulation persistence.
type Store struct{ Pool *pgxpool.Pool }

// Create is a bounded command for the currently supported scenario.
type Create struct {
	DelayMS         int    `json:"delay_ms,omitempty"`
	Environment     string `json:"environment"`
	Scenario        string `json:"scenario"`
	Percentage      int    `json:"percentage"`
	DurationSeconds int    `json:"duration_seconds"`
	Reason          string `json:"reason"`
}

// Run records a scheduled expiry even when no worker is running.
type Run struct {
	DelayMS      int        `json:"delay_ms"`
	ID           string     `json:"id"`
	DeploymentID string     `json:"deployment_id"`
	Environment  string     `json:"environment"`
	Scenario     string     `json:"scenario"`
	Percentage   int        `json:"percentage"`
	StartedAt    time.Time  `json:"started_at"`
	ExpiresAt    time.Time  `json:"expires_at"`
	StoppedAt    *time.Time `json:"stopped_at"`
	Reason       string     `json:"reason"`
	Active       bool       `json:"active"`
}

const columns = `id,COALESCE(deployment_id,''),environment,scenario,delay_ms,percentage,started_at,expires_at,stopped_at,reason,(stopped_at IS NULL AND expires_at>clock_timestamp())`

func scan(row pgx.Row) (Run, error) {
	var r Run
	err := row.Scan(&r.ID, &r.DeploymentID, &r.Environment, &r.Scenario, &r.DelayMS, &r.Percentage, &r.StartedAt, &r.ExpiresAt, &r.StoppedAt, &r.Reason, &r.Active)
	return r, err
}
func validEnvironment(v string) bool { return v == "development" || v == "staging" }

// Create starts at most one active run per environment, with exact-command replay.
func (s Store) Create(ctx context.Context, in Create, key string) (Run, bool, error) {
	return s.CreateAs(ctx, in, key, "local-operator")
}

// CreateAs records the authenticated operator who started a run.
func (s Store) CreateAs(ctx context.Context, in Create, key, actor string) (Run, bool, error) {
	validScenario := ((in.Scenario == "payment-decline" || in.Scenario == "bad-deployment") && in.DelayMS == 0) || ((in.Scenario == "database-latency" || in.Scenario == "kafka-consumer-lag" || in.Scenario == "database-timeout") && in.DelayMS >= 100 && in.DelayMS <= 4500)
	if !validEnvironment(in.Environment) || !validScenario || in.Percentage < 1 || in.Percentage > 100 || in.DurationSeconds < 30 || in.DurationSeconds > 900 || len(strings.TrimSpace(in.Reason)) < 1 || len(in.Reason) > 1000 || len(key) < 16 || len(key) > 100 {
		return Run{}, false, ErrInvalid
	}
	payload, err := json.Marshal(in)
	if err != nil {
		return Run{}, false, err
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return Run{}, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// Lock the command key before environment so cross-environment key misuse is serialized.
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,20))", key); err != nil {
		return Run{}, false, err
	}
	var id string
	var same bool
	err = tx.QueryRow(ctx, "SELECT id,request=$2::jsonb FROM simulation.runs WHERE creation_key=$1", key, payload).Scan(&id, &same)
	if err == nil {
		if !same {
			return Run{}, false, ErrConflict
		}
		run, e := scan(tx.QueryRow(ctx, "SELECT "+columns+" FROM simulation.runs WHERE id=$1", id))
		return run, true, e
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Run{}, false, err
	}
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,21))", in.Environment); err != nil {
		return Run{}, false, err
	}
	var active bool
	if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM simulation.runs WHERE environment=$1 AND stopped_at IS NULL AND expires_at>clock_timestamp())", in.Environment).Scan(&active); err != nil {
		return Run{}, false, err
	}
	if active {
		return Run{}, false, ErrConflict
	}
	token, err := domain.NewID(12)
	if err != nil {
		return Run{}, false, err
	}
	id = "SIM-" + token
	deploymentID := ""
	if in.Scenario == "bad-deployment" {
		deploymentID = "sim-deploy-" + token
	}
	run, err := scan(tx.QueryRow(ctx, "INSERT INTO simulation.runs(id,creation_key,request,environment,percentage,expires_at,reason,scenario,delay_ms,deployment_id) VALUES($1,$2,$3,$4,$5,clock_timestamp()+make_interval(secs=>$6),$7,$8,$9,NULLIF($10,'')) RETURNING "+columns, id, key, payload, in.Environment, in.Percentage, in.DurationSeconds, in.Reason, in.Scenario, in.DelayMS, deploymentID))
	if err != nil {
		return Run{}, false, err
	}
	if deploymentID != "" {
		if _, err = tx.Exec(ctx, "INSERT INTO simulation.deployment_outbox(run_id,status,event_id,occurred_at) VALUES($1,'Completed',$2,$3)", id, "sim-event-"+token+"-completed", run.StartedAt); err != nil {
			return Run{}, false, err
		}
	}
	if _, err = tx.Exec(ctx, "INSERT INTO simulation.audit(run_id,action,actor,note) VALUES($1,'started',$2,$3)", id, actor, in.Reason); err != nil {
		return Run{}, false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Run{}, false, err
	}
	return run, false, nil
}

// Stop is idempotent; stopping never changes already recorded decisions.
func (s Store) Stop(ctx context.Context, id, note string) (Run, error) {
	return s.StopAs(ctx, id, note, "local-operator")
}

// StopAs records the authenticated operator who stopped a run.
func (s Store) StopAs(ctx context.Context, id, note, actor string) (Run, error) {
	if len(strings.TrimSpace(note)) < 1 || len(note) > 1000 {
		return Run{}, ErrInvalid
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return Run{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	run, err := scan(tx.QueryRow(ctx, "SELECT "+columns+" FROM simulation.runs WHERE id=$1 FOR UPDATE", id))
	if err != nil {
		return Run{}, err
	}
	if run.StoppedAt != nil {
		return run, nil
	}
	run, err = scan(tx.QueryRow(ctx, "UPDATE simulation.runs SET stopped_at=clock_timestamp() WHERE id=$1 RETURNING "+columns, id))
	if err != nil {
		return Run{}, err
	}
	if _, err = tx.Exec(ctx, "INSERT INTO simulation.audit(run_id,action,actor,note) VALUES($1,'stopped',$2,$3)", id, actor, note); err != nil {
		return Run{}, err
	}
	if run.DeploymentID != "" {
		if _, err = tx.Exec(ctx, "INSERT INTO simulation.deployment_outbox(run_id,status,event_id,occurred_at) VALUES($1,'Rolled Back',$2,$3) ON CONFLICT(run_id,status) DO NOTHING", id, "sim-event-"+strings.TrimPrefix(id, "SIM-")+"-rollback", *run.StoppedAt); err != nil {
			return Run{}, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return Run{}, err
	}
	return run, nil
}

// List returns the latest twenty runs, including derived active/expired status.
func (s Store) List(ctx context.Context, environment string) ([]Run, error) {
	if !validEnvironment(environment) {
		return nil, ErrInvalid
	}
	rows, err := s.Pool.Query(ctx, "SELECT "+columns+" FROM simulation.runs WHERE environment=$1 ORDER BY started_at DESC,id DESC LIMIT 20", environment)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Run{}
	for rows.Next() {
		r, e := scan(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Decide freezes the first decision for a transaction, including no-injection decisions.
func (s Store) Decide(ctx context.Context, in faults.Request) (faults.Decision, error) {
	if !validEnvironment(in.Environment) || !transactionID.MatchString(in.TransactionID) {
		return faults.Decision{}, ErrInvalid
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return faults.Decision{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,22))", in.TransactionID); err != nil {
		return faults.Decision{}, err
	}
	var out faults.Decision
	var environment string
	err = tx.QueryRow(ctx, "SELECT COALESCE(d.run_id,''),d.inject,d.environment,COALESCE(r.scenario,''),COALESCE(r.delay_ms,0),COALESCE(r.deployment_id,''),COALESCE(r.stopped_at IS NULL AND r.expires_at>clock_timestamp(),false) FROM simulation.decisions d LEFT JOIN simulation.runs r ON r.id=d.run_id WHERE d.transaction_id=$1", in.TransactionID).Scan(&out.RunID, &out.Inject, &environment, &out.Scenario, &out.DelayMS, &out.DeploymentID, &out.Active)
	if err == nil {
		if environment != in.Environment {
			return out, ErrConflict
		}
		if out.Scenario == "bad-deployment" {
			out.Version = syntheticBadVersion
		}
		return out, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return out, err
	}
	// Lock a selected run against stop until its decision is committed.
	var percentage int
	err = tx.QueryRow(ctx, "SELECT id,percentage,scenario,delay_ms,COALESCE(deployment_id,'') FROM simulation.runs r WHERE environment=$1 AND stopped_at IS NULL AND expires_at>clock_timestamp() AND (scenario<>'bad-deployment' OR EXISTS(SELECT 1 FROM simulation.deployment_outbox o WHERE o.run_id=r.id AND o.status='Completed' AND o.delivered_at IS NOT NULL)) ORDER BY started_at DESC LIMIT 1 FOR SHARE", in.Environment).Scan(&out.RunID, &percentage, &out.Scenario, &out.DelayMS, &out.DeploymentID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return out, err
	}
	if out.RunID != "" {
		out.Active = true
		if out.Scenario == "bad-deployment" {
			out.Version = syntheticBadVersion
		}
		sum := sha256.Sum256([]byte(out.RunID + ":" + in.TransactionID))
		out.Inject = int(binary.BigEndian.Uint64(sum[:8])%100) < percentage
	}
	if _, err = tx.Exec(ctx, "INSERT INTO simulation.decisions(transaction_id,environment,run_id,inject) VALUES($1,$2,NULLIF($3,''),$4)", in.TransactionID, in.Environment, out.RunID, out.Inject); err != nil {
		return out, err
	}
	if err = tx.Commit(ctx); err != nil {
		return out, err
	}
	return out, nil
}
