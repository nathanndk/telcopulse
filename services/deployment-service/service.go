// Package deployment persists CI-reported deployment events for operator correlation.
package deployment

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"telcopulse/services/shared/runtime"
)

var (
	ErrInvalid      = errors.New("invalid deployment event or query")
	ErrConflict     = errors.New("deployment event conflicts with stored history")
	ErrNotFound     = errors.New("deployment not found")
	idPattern       = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._:-]{7,99}$`)
	servicePattern  = regexp.MustCompile(`^[a-z][a-z0-9-]{1,98}[a-z0-9]$`)
	deployerPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._@:-]{0,99}$`)
	versionPattern  = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._+-]{0,79}$`)
	commitPattern   = regexp.MustCompile(`^[a-f0-9]{7,64}$`)
)

type Event struct {
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

type Record struct {
	ID          string    `json:"id"`
	Service     string    `json:"service"`
	Version     string    `json:"version"`
	CommitSHA   string    `json:"commit_sha"`
	Environment string    `json:"environment"`
	Deployer    string    `json:"deployer"`
	Status      string    `json:"status"`
	OccurredAt  time.Time `json:"occurred_at"`
}

type Detail struct {
	Record
	Events []EventState `json:"events"`
}

type EventState struct {
	EventID    string    `json:"event_id"`
	Actor      string    `json:"actor"`
	Status     string    `json:"status"`
	OccurredAt time.Time `json:"occurred_at"`
}

func (e Event) validate(now time.Time) error {
	if !idPattern.MatchString(e.EventID) || !idPattern.MatchString(e.DeploymentID) ||
		!servicePattern.MatchString(e.Service) || !versionPattern.MatchString(e.Version) ||
		!commitPattern.MatchString(e.CommitSHA) || !deployerPattern.MatchString(e.Deployer) ||
		(e.Environment != "development" && e.Environment != "staging" && e.Environment != "production") ||
		!validStatus(e.Status) || e.OccurredAt.IsZero() || e.OccurredAt.After(now.Add(5*time.Minute)) {
		return ErrInvalid
	}
	return nil
}

func validStatus(status string) bool {
	switch status {
	case "Pending", "Running", "Completed", "Failed", "Rolled Back":
		return true
	default:
		return false
	}
}

func nextStatus(before, after string) bool {
	switch before {
	case "Pending":
		return after == "Running" || after == "Completed" || after == "Failed"
	case "Running":
		return after == "Completed" || after == "Failed"
	case "Completed":
		return after == "Rolled Back"
	default:
		return false
	}
}

type Store struct{ Pool *pgxpool.Pool }

// Report serializes updates for one deployment and deduplicates immutable event IDs.
func (s Store) Report(ctx context.Context, e Event) (Record, bool, error) {
	var out Record
	if err := e.validate(time.Now()); err != nil {
		return out, false, err
	}
	e.OccurredAt = e.OccurredAt.UTC()
	payload, err := json.Marshal(e)
	if err != nil {
		return out, false, err
	}
	hash := sha256.Sum256(payload)
	digest := hex.EncodeToString(hash[:])
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return out, false, err
	}
	defer func() {
		rollbackCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = tx.Rollback(rollbackCtx)
	}()
	// A transaction-scoped lock also covers two simultaneous first events.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,1))`, e.EventID); err != nil {
		return out, false, err
	}
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, e.DeploymentID); err != nil {
		return out, false, err
	}
	var savedHash, savedDeployment string
	err = tx.QueryRow(ctx, `SELECT request_hash,deployment_id FROM deployment.events WHERE event_id=$1`, e.EventID).Scan(&savedHash, &savedDeployment)
	if err == nil {
		if savedHash != digest || savedDeployment != e.DeploymentID {
			return out, false, ErrConflict
		}
		out, err = record(ctx, tx, e.DeploymentID)
		if err != nil {
			return out, false, err
		}
		return out, true, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return out, false, err
	}
	out, err = record(ctx, tx, e.DeploymentID)
	if errors.Is(err, pgx.ErrNoRows) {
		if e.Status == "Rolled Back" {
			return out, false, ErrInvalid
		}
		_, err = tx.Exec(ctx, `INSERT INTO deployment.records(id,service,environment,version,commit_sha,deployer,status,occurred_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, e.DeploymentID, e.Service, e.Environment, e.Version, e.CommitSHA, e.Deployer, e.Status, e.OccurredAt)
		if err != nil {
			return out, false, err
		}
		out = Record{ID: e.DeploymentID, Service: e.Service, Version: e.Version, CommitSHA: e.CommitSHA, Environment: e.Environment, Deployer: e.Deployer, Status: e.Status, OccurredAt: e.OccurredAt}
	} else if err != nil {
		return out, false, err
	} else {
		if out.Service != e.Service || out.Environment != e.Environment || out.Version != e.Version || out.CommitSHA != e.CommitSHA || !nextStatus(out.Status, e.Status) || !e.OccurredAt.After(out.OccurredAt) {
			return out, false, ErrConflict
		}
		_, err = tx.Exec(ctx, `UPDATE deployment.records SET status=$2,occurred_at=$3 WHERE id=$1`, e.DeploymentID, e.Status, e.OccurredAt)
		if err != nil {
			return out, false, err
		}
		out.Status = e.Status
		out.OccurredAt = e.OccurredAt
	}
	_, err = tx.Exec(ctx, `INSERT INTO deployment.events(event_id,deployment_id,request_hash,actor,status,occurred_at) VALUES($1,$2,$3,$4,$5,$6)`, e.EventID, e.DeploymentID, digest, e.Deployer, e.Status, e.OccurredAt)
	if err != nil {
		return out, false, err
	}
	return out, false, tx.Commit(ctx)
}

func record(ctx context.Context, tx pgx.Tx, id string) (Record, error) {
	var r Record
	err := tx.QueryRow(ctx, `SELECT id,service,version,commit_sha,environment,deployer,status,occurred_at FROM deployment.records WHERE id=$1`, id).Scan(&r.ID, &r.Service, &r.Version, &r.CommitSHA, &r.Environment, &r.Deployer, &r.Status, &r.OccurredAt)
	return r, err
}

func (s Store) Get(ctx context.Context, id string) (Detail, error) {
	var out Detail
	if !idPattern.MatchString(id) {
		return out, ErrInvalid
	}
	err := s.Pool.QueryRow(ctx, `SELECT id,service,version,commit_sha,environment,deployer,status,occurred_at FROM deployment.records WHERE id=$1`, id).Scan(&out.ID, &out.Service, &out.Version, &out.CommitSHA, &out.Environment, &out.Deployer, &out.Status, &out.OccurredAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, ErrNotFound
	}
	if err != nil {
		return out, err
	}
	out.Events = []EventState{}
	rows, err := s.Pool.Query(ctx, `SELECT event_id,actor,status,occurred_at FROM deployment.events WHERE deployment_id=$1 ORDER BY occurred_at,event_id LIMIT 100`, id)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var e EventState
		if err = rows.Scan(&e.EventID, &e.Actor, &e.Status, &e.OccurredAt); err != nil {
			return out, err
		}
		out.Events = append(out.Events, e)
	}
	return out, rows.Err()
}

// List returns recent matching deployments, including before/after windows for incidents.
func (s Store) List(ctx context.Context, environment, service string, since, until time.Time, limit int) ([]Record, error) {
	if (environment != "development" && environment != "staging" && environment != "production") || (service != "" && !servicePattern.MatchString(service)) || limit < 1 || limit > 100 || since.IsZero() || until.IsZero() || !since.Before(until) || until.Sub(since) > 30*24*time.Hour {
		return nil, ErrInvalid
	}
	rows, err := s.Pool.Query(ctx, `SELECT r.id,r.service,r.version,r.commit_sha,r.environment,r.deployer,r.status,r.occurred_at FROM deployment.records r WHERE r.environment=$1 AND ($2='' OR r.service=$2) AND EXISTS (SELECT 1 FROM deployment.events e WHERE e.deployment_id=r.id AND e.occurred_at >= $3 AND e.occurred_at <= $4) ORDER BY r.occurred_at DESC,r.id DESC LIMIT $5`, environment, service, since, until, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Record{}
	for rows.Next() {
		var r Record
		if err = rows.Scan(&r.ID, &r.Service, &r.Version, &r.CommitSHA, &r.Environment, &r.Deployer, &r.Status, &r.OccurredAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func Handler(pool *pgxpool.Pool, log *slog.Logger) http.Handler {
	store := Store{Pool: pool}
	mux := http.NewServeMux()
	fail := func(w http.ResponseWriter, err error) {
		switch {
		case errors.Is(err, ErrInvalid):
			runtime.JSON(w, 422, map[string]string{"error": "invalid deployment event or query"})
		case errors.Is(err, ErrConflict):
			runtime.JSON(w, 409, map[string]string{"error": "deployment event conflicts with stored history"})
		case errors.Is(err, ErrNotFound):
			runtime.JSON(w, 404, map[string]string{"error": "deployment not found"})
		default:
			log.Error("deployment persistence failed", "error", err)
			runtime.JSON(w, 503, map[string]string{"error": "deployment service unavailable"})
		}
	}
	mux.HandleFunc("POST /internal/deployments/events", func(w http.ResponseWriter, r *http.Request) {
		var e Event
		if !runtime.Decode(w, r, &e) {
			return
		}
		out, replay, err := store.Report(r.Context(), e)
		if err != nil {
			fail(w, err)
			return
		}
		status := 201
		if replay {
			status = 200
		}
		runtime.JSON(w, status, out)
	})
	mux.HandleFunc("GET /internal/deployments/operations", func(w http.ResponseWriter, r *http.Request) {
		out, err := store.Operations(r.Context(), r.URL.Query().Get("environment"))
		if err != nil {
			fail(w, err)
			return
		}
		runtime.JSON(w, 200, out)
	})
	mux.HandleFunc("GET /internal/deployments/{id}", func(w http.ResponseWriter, r *http.Request) {
		out, err := store.Get(r.Context(), r.PathValue("id"))
		if err != nil {
			fail(w, err)
			return
		}
		runtime.JSON(w, 200, out)
	})
	mux.HandleFunc("GET /internal/deployments", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		now := time.Now().UTC()
		since := now.Add(-24 * time.Hour)
		until := now
		if raw := q.Get("since"); raw != "" {
			parsed, err := time.Parse(time.RFC3339Nano, raw)
			if err != nil {
				fail(w, ErrInvalid)
				return
			}
			since = parsed
		}
		if raw := q.Get("until"); raw != "" {
			parsed, err := time.Parse(time.RFC3339Nano, raw)
			if err != nil {
				fail(w, ErrInvalid)
				return
			}
			until = parsed
		}
		limit := 50
		if raw := q.Get("limit"); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil {
				fail(w, ErrInvalid)
				return
			}
			limit = parsed
		}
		environment := q.Get("environment")
		if environment == "" {
			environment = "development"
		}
		out, err := store.List(r.Context(), environment, q.Get("service"), since, until, limit)
		if err != nil {
			fail(w, err)
			return
		}
		runtime.JSON(w, 200, out)
	})
	return mux
}
