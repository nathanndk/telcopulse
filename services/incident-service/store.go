package incident

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"log/slog"
	"regexp"
	"slices"
	"strings"
	"telcopulse/services/shared/domain"
	"time"
)

var (
	ErrInvalid   = errors.New("invalid incident request")
	ErrConflict  = errors.New("incident revision or creation key conflict")
	ErrNotFound  = errors.New("incident not found")
	ErrForbidden = errors.New("incident change forbidden for role")
)
var incidentIDPattern = regexp.MustCompile(`^INC-[a-f0-9]{24}$`)
var keyPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{16,100}$`)

// Store provides atomic incident and audit persistence.
type Store struct{ Pool *pgxpool.Pool }

func invalid(err error) error { return fmt.Errorf("%w: %v", ErrInvalid, err) }
func rollback(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
		slog.Error("release incident transaction", "error", err)
	}
}

// Create deduplicates an immutable creation command and returns the latest record on replay.
func (s Store) Create(ctx context.Context, input Create, key, actor string) (Incident, bool, error) {
	var out Incident
	if err := validateFields(input.Fields); err != nil {
		return out, false, invalid(err)
	}
	input.ActionItems, _ = normalizeActionItems(input.ActionItems)
	if !keyPattern.MatchString(key) || !bounded(actor, 1, 100) || !bounded(input.Service, 1, 100) || !slices.Contains([]string{"development", "staging"}, input.Environment) {
		return out, false, invalid(errors.New("invalid key, actor, service or environment"))
	}
	now := time.Now().UTC()
	if input.DetectedAt != nil && (input.DetectedAt.IsZero() || input.DetectedAt.After(now.Add(time.Minute))) {
		return out, false, invalid(errors.New("invalid detection time"))
	}
	request, err := json.Marshal(struct {
		Input Create
		Actor string
	}{input, actor})
	if err != nil {
		return out, false, invalid(err)
	}
	hash := sha256.Sum256(request)
	digest := hex.EncodeToString(hash[:])
	id, err := domain.NewID(12)
	if err != nil {
		return out, false, err
	}
	out = Incident{Fields: input.Fields, ID: "INC-" + id, Environment: input.Environment, Service: input.Service, State: Detected, Version: 1, CreatedAt: now, DetectedAt: now, UpdatedAt: now}
	if input.DetectedAt != nil {
		out.DetectedAt = *input.DetectedAt
	}
	document, err := json.Marshal(out)
	if err != nil {
		return out, false, err
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return out, false, err
	}
	defer rollback(tx)
	tag, err := tx.Exec(ctx, `INSERT INTO incident.records(id,creation_key,request_hash,environment,state,version,document,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(creation_key) DO NOTHING`, out.ID, key, digest, out.Environment, out.State, out.Version, document, now)
	if err != nil {
		return out, false, err
	}
	if tag.RowsAffected() == 0 {
		var saved string
		if err = tx.QueryRow(ctx, `SELECT request_hash,document FROM incident.records WHERE creation_key=$1`, key).Scan(&saved, &document); err != nil {
			return out, false, err
		}
		if saved != digest {
			return out, false, ErrConflict
		}
		if err = json.Unmarshal(document, &out); err != nil {
			return out, false, err
		}
		return out, true, tx.Commit(ctx)
	}
	if err = appendAudit(ctx, tx, Audit{Version: 1, Action: "created", Actor: actor, Note: "Incident detected", At: now, After: out}); err != nil {
		return out, false, err
	}
	return out, false, tx.Commit(ctx)
}
func appendAudit(ctx context.Context, tx pgx.Tx, entry Audit) error {
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO incident.audit(incident_id,version,entry) VALUES($1,$2,$3)`, entry.After.ID, entry.Version, data)
	return err
}

// Get reads the snapshot and history from one PostgreSQL statement snapshot.
func (s Store) Get(ctx context.Context, id string) (Detail, error) {
	return s.GetHistory(ctx, id, 0)
}

// GetHistory returns at most ten immutable revisions after the supplied version.
func (s Store) GetHistory(ctx context.Context, id string, after int64) (Detail, error) {
	var out Detail
	if after < 0 {
		return out, invalid(errors.New("invalid history cursor"))
	}
	var document, history, postmortem []byte
	err := s.Pool.QueryRow(ctx, `SELECT document,COALESCE((SELECT jsonb_agg(entry ORDER BY version) FROM (SELECT version,entry FROM incident.audit WHERE incident_id=r.id AND version>$2 ORDER BY version LIMIT 11) a),'[]'::jsonb),(SELECT document FROM incident.postmortems WHERE incident_id=r.id) FROM incident.records r WHERE id=$1`, id, after).Scan(&document, &history, &postmortem)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, ErrNotFound
	}
	if err != nil {
		return out, err
	}
	if err = json.Unmarshal(document, &out.Incident); err != nil {
		return out, err
	}
	if len(postmortem) > 0 {
		out.Postmortem = &PostmortemReport{}
		if err = json.Unmarshal(postmortem, out.Postmortem); err != nil {
			return out, err
		}
	}
	err = json.Unmarshal(history, &out.History)
	if len(out.History) > 10 {
		out.HistoryMore = true
		out.History = out.History[:10]
		out.HistoryNext = int64(out.History[9].Version)
	}
	return out, err
}

// Update rejects stale edits and commits each revision with its before/after history.
func (s Store) Update(ctx context.Context, id, actor string, input Update) (Incident, error) {
	return s.UpdateAs(ctx, id, actor, "", input)
}

// UpdateAs authorizes a validated role against the locked current revision.
// Empty role is reserved for the local service-only development path.
func (s Store) UpdateAs(ctx context.Context, id, actor, role string, input Update) (Incident, error) {
	var current Incident
	if input.ExpectedVersion < 1 || !bounded(actor, 1, 100) {
		return current, invalid(errors.New("actor and expected_version are required"))
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return current, err
	}
	defer rollback(tx)
	var data []byte
	err = tx.QueryRow(ctx, `SELECT document FROM incident.records WHERE id=$1 FOR UPDATE`, id).Scan(&data)
	if errors.Is(err, pgx.ErrNoRows) {
		return current, ErrNotFound
	}
	if err != nil {
		return current, err
	}
	if err = json.Unmarshal(data, &current); err != nil {
		return current, err
	}
	if current.Version != input.ExpectedVersion {
		return current, ErrConflict
	}
	if current.State == Resolved && input.State == Postmortem &&
		(current.Severity == "SEV-1" || current.Severity == "SEV-2") {
		return current, invalid(errors.New("generate a structured postmortem for major incidents"))
	}
	if err = authorizeUpdate(current, input, role); err != nil {
		return current, err
	}
	next, err := Apply(current, input, time.Now().UTC())
	if err != nil {
		return current, invalid(err)
	}
	if next.RecoveryValidation != nil && current.State == Monitoring && next.State == Resolved {
		next.RecoveryValidation.ValidatedBy = actor
	}
	data, err = json.Marshal(next)
	if err != nil {
		return current, invalid(err)
	}
	if _, err = tx.Exec(ctx, `UPDATE incident.records SET document=$2,version=$3,state=$4,updated_at=$5 WHERE id=$1`, id, data, next.Version, next.State, next.UpdatedAt); err != nil {
		return current, err
	}
	if err = appendAudit(ctx, tx, Audit{Version: next.Version, Action: "updated", Actor: actor, Note: input.Note, At: next.UpdatedAt, Before: &current, After: next}); err != nil {
		return current, err
	}
	return next, tx.Commit(ctx)
}

// EscalateAs atomically records a Commander coordination handoff. It does not
// claim that an external on-call notification was delivered.
func (s Store) EscalateAs(ctx context.Context, id, actor, role string, input Escalate) (Incident, error) {
	var current Incident
	if role != "" && role != "Incident Commander" && role != "Administrator" {
		return current, ErrForbidden
	}
	if input.ExpectedVersion < 1 || !bounded(actor, 1, 100) || !bounded(input.Team, 1, 100) ||
		len(input.Owner) > 100 || !bounded(input.Reason, 1, 2000) {
		return current, invalid(errors.New("team, reason and expected_version are required"))
	}
	if input.Severity != "" && !slices.Contains([]string{"SEV-1", "SEV-2", "SEV-3", "SEV-4"}, input.Severity) {
		return current, invalid(errors.New("invalid escalation severity"))
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return current, err
	}
	defer rollback(tx)
	var data []byte
	err = tx.QueryRow(ctx, "SELECT document FROM incident.records WHERE id=$1 FOR UPDATE", id).Scan(&data)
	if errors.Is(err, pgx.ErrNoRows) {
		return current, ErrNotFound
	}
	if err != nil {
		return current, err
	}
	if err = json.Unmarshal(data, &current); err != nil {
		return current, err
	}
	if current.Version != input.ExpectedVersion {
		return current, ErrConflict
	}
	if current.State == Resolved || current.State == Postmortem || current.EscalationLevel >= 20 {
		return current, invalid(errors.New("incident cannot be escalated in its current state"))
	}
	if input.Severity != "" {
		ordered := []string{"SEV-1", "SEV-2", "SEV-3", "SEV-4"}
		if slices.Index(ordered, input.Severity) > slices.Index(ordered, current.Severity) {
			return current, invalid(errors.New("escalation cannot lower severity"))
		}
	}
	next := current
	now := time.Now().UTC()
	next.OwningTeam = input.Team
	next.EscalationLevel++
	next.EscalatedAt = &now
	if input.Owner != "" {
		next.Owner = input.Owner
	}
	if input.Severity != "" {
		next.Severity = input.Severity
	}
	next.Version++
	next.UpdatedAt = now
	data, err = json.Marshal(next)
	if err != nil {
		return current, err
	}
	if _, err = tx.Exec(ctx, `UPDATE incident.records SET document=$2,version=$3,updated_at=$4 WHERE id=$1`, id, data, next.Version, next.UpdatedAt); err != nil {
		return current, err
	}
	if err = appendAudit(ctx, tx, Audit{Version: next.Version, Action: "escalated", Actor: actor, Note: input.Reason, At: now, Before: &current, After: next}); err != nil {
		return current, err
	}
	return next, tx.Commit(ctx)
}

// Page is a bounded latest-incidents view; More signals additional older records.
type Page struct {
	Items []Summary `json:"items"`
	More  bool      `json:"more"`
	Next  string    `json:"next,omitempty"`
}

// List filters exact environment/state and returns at most 100 incidents.
func (s Store) List(ctx context.Context, environment string, state State, limit int) (Page, error) {
	return s.ListAfter(ctx, environment, state, limit, "")
}

type listCursor struct {
	At           time.Time
	ID           string
	Sort         string
	SortSeverity string
	Environment  string
	State        State
	Severity     string
	Service      string
	Owner        string
	Search       string
	Since        string
}

type ListFilter struct {
	Environment string
	State       State
	Severity    string
	Service     string
	Owner       string
	Search      string
	Since       string
	Sort        string
	Limit       int
	Cursor      string
}

// ListAfter uses an opaque, filter-scoped keyset cursor. Concurrent edits may move
// records ahead of the cursor; callers should refresh the first page for live changes.
func (s Store) ListAfter(ctx context.Context, environment string, state State, limit int, cursor string) (Page, error) {
	return s.ListFiltered(ctx, ListFilter{Environment: environment, State: state, Limit: limit, Cursor: cursor})
}

func (s Store) ListFiltered(ctx context.Context, filter ListFilter) (Page, error) {
	out := Page{Items: []Summary{}}
	if filter.Sort == "" {
		filter.Sort = "updated_desc"
	}
	if !slices.Contains([]string{"updated_desc", "detected_desc", "detected_asc", "severity_asc"}, filter.Sort) {
		return out, invalid(errors.New("invalid incident sort"))
	}
	filter.Service = strings.TrimSpace(filter.Service)
	filter.Owner = strings.TrimSpace(filter.Owner)
	filter.Search = strings.TrimSpace(filter.Search)
	var since *time.Time
	if filter.Since != "" {
		if len(filter.Since) > 64 {
			return out, invalid(errors.New("invalid detected-time filter"))
		}
		parsed, err := time.Parse(time.RFC3339, filter.Since)
		if err != nil {
			return out, invalid(errors.New("invalid detected-time filter"))
		}
		parsed = parsed.UTC()
		filter.Since = parsed.Format(time.RFC3339Nano)
		since = &parsed
	}
	var boundary listCursor
	if len(filter.Cursor) > 1024 {
		return out, invalid(errors.New("invalid list cursor"))
	}
	if filter.Cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(filter.Cursor)
		if err != nil || json.Unmarshal(raw, &boundary) != nil || boundary.At.IsZero() || !incidentIDPattern.MatchString(boundary.ID) || boundary.Environment != filter.Environment || boundary.State != filter.State || boundary.Severity != filter.Severity || boundary.Service != filter.Service || boundary.Owner != filter.Owner || boundary.Search != filter.Search || boundary.Since != filter.Since || (boundary.Sort != filter.Sort && !(boundary.Sort == "" && filter.Sort == "updated_desc")) || (filter.Sort == "severity_asc" && !slices.Contains([]string{"SEV-1", "SEV-2", "SEV-3", "SEV-4"}, boundary.SortSeverity)) {
			return out, invalid(errors.New("invalid list cursor"))
		}
	}
	if !slices.Contains([]string{"development", "staging"}, filter.Environment) || filter.Limit < 1 || filter.Limit > 100 || len(filter.Service) > 100 || len(filter.Owner) > 100 || len(filter.Search) > 100 || (filter.Severity != "" && !slices.Contains([]string{"SEV-1", "SEV-2", "SEV-3", "SEV-4"}, filter.Severity)) {
		return out, invalid(errors.New("invalid incident list filters"))
	}
	if _, ok := transitions[filter.State]; filter.State != "" && !ok {
		return out, invalid(errors.New("unknown state"))
	}
	order := `AND ($8='' OR (updated_at,id)<($9,$8)) ORDER BY updated_at DESC,id DESC`
	selectedTime := `updated_at`
	if filter.Sort == "detected_desc" {
		order = `AND ($8='' OR (detected_at,id)<($9,$8)) ORDER BY detected_at DESC,id DESC`
		selectedTime = `detected_at`
	} else if filter.Sort == "detected_asc" {
		order = `AND ($8='' OR (detected_at,id)>($9,$8)) ORDER BY detected_at ASC,id ASC`
		selectedTime = `detected_at`
	} else if filter.Sort == "severity_asc" {
		order = `AND ($8='' OR (document->>'severity')>$11 OR ((document->>'severity')=$11 AND (updated_at,id)<($9,$8))) ORDER BY document->>'severity' ASC,updated_at DESC,id DESC`
	}
	args := []any{filter.Environment, filter.State, filter.Severity, filter.Service, filter.Owner, filter.Search, since, boundary.ID, boundary.At, filter.Limit + 1}
	if filter.Sort == "severity_asc" {
		args = append(args, boundary.SortSeverity)
	}
	rows, err := s.Pool.Query(ctx, `SELECT document - '{impact,root_cause,mitigation,resolution,postmortem_notes,evidence,action_items}'::text[],`+selectedTime+` FROM incident.records
	WHERE environment=$1 AND ($2='' OR state=$2)
	  AND ($3='' OR document->>'severity'=$3)
	  AND ($4='' OR document->>'service'=$4)
	  AND ($5='' OR strpos(lower(document->>'owner'),lower($5))>0)
	  AND ($6='' OR strpos(lower(document->>'title'),lower($6))>0 OR strpos(lower(id),lower($6))>0)
	  AND ($7::timestamptz IS NULL OR detected_at >= $7)
	  `+order+` LIMIT $10`, args...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	var lastTime time.Time
	for rows.Next() {
		var b []byte
		var item Summary
		if err = rows.Scan(&b, &lastTime); err != nil {
			return out, err
		}
		if err = json.Unmarshal(b, &item); err != nil {
			return out, err
		}
		out.Items = append(out.Items, item)
		if len(out.Items) == filter.Limit {
			boundary = listCursor{At: lastTime, ID: item.ID, Sort: filter.Sort, SortSeverity: item.Severity, Environment: filter.Environment, State: filter.State,
				Severity: filter.Severity, Service: filter.Service, Owner: filter.Owner, Search: filter.Search, Since: filter.Since}
		}
	}
	if err = rows.Err(); err != nil {
		return out, err
	}
	if len(out.Items) > filter.Limit {
		out.More = true
		out.Items = out.Items[:filter.Limit]
		data, err := json.Marshal(boundary)
		if err != nil {
			return out, err
		}
		out.Next = base64.RawURLEncoding.EncodeToString(data)
	}
	return out, nil
}
