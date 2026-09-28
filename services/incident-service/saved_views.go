package incident

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strings"
	"telcopulse/services/shared/domain"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var savedViewID = regexp.MustCompile(`^VIEW-[a-f0-9]{24}$`)
var ErrSavedViewConflict = errors.New("saved view name already exists in this environment")

// SavedViewFilters intentionally stores a relative range, never a stale absolute timestamp or page cursor.
type SavedViewFilters struct {
	State    string   `json:"state"`
	Severity string   `json:"severity"`
	Service  string   `json:"service"`
	Owner    string   `json:"owner"`
	Search   string   `json:"search"`
	Range    string   `json:"range"`
	Sort     string   `json:"sort"`
	Columns  []string `json:"columns"`
}
type SavedViewInput struct {
	Environment string           `json:"environment"`
	Name        string           `json:"name"`
	Filters     SavedViewFilters `json:"filters"`
}
type SavedView struct {
	ID          string           `json:"id"`
	Environment string           `json:"environment"`
	Name        string           `json:"name"`
	Filters     SavedViewFilters `json:"filters"`
	CreatedAt   time.Time        `json:"created_at"`
	UpdatedAt   time.Time        `json:"updated_at"`
}

func validateSavedView(input *SavedViewInput) error {
	input.Name = strings.TrimSpace(input.Name)
	input.Filters.Service = strings.TrimSpace(input.Filters.Service)
	input.Filters.Owner = strings.TrimSpace(input.Filters.Owner)
	input.Filters.Search = strings.TrimSpace(input.Filters.Search)
	f := input.Filters
	if f.Sort == "" {
		f.Sort = "updated_desc"
	}
	if len(f.Columns) == 0 {
		f.Columns = []string{"incident", "severity", "state", "service", "owner", "owning_team", "detected_at", "updated_at", "actions"}
	}
	input.Filters = f
	seen := map[string]bool{}
	for _, column := range f.Columns {
		if !slices.Contains([]string{"incident", "severity", "state", "service", "owner", "owning_team", "detected_at", "updated_at", "actions"}, column) || seen[column] {
			return invalid(errors.New("invalid saved view columns"))
		}
		seen[column] = true
	}
	if !slices.Contains([]string{"development", "staging"}, input.Environment) || len(input.Name) < 1 || len(input.Name) > 60 || strings.ContainsAny(input.Name, "\r\n\t") ||
		!seen["incident"] || !slices.Contains([]string{"updated_desc", "detected_desc", "detected_asc", "severity_asc"}, f.Sort) ||
		(f.State != "" && !slices.Contains([]string{"Detected", "Acknowledged", "Investigating", "Identified", "Mitigating", "Monitoring", "Resolved", "Postmortem"}, f.State)) ||
		(f.Severity != "" && !slices.Contains([]string{"SEV-1", "SEV-2", "SEV-3", "SEV-4"}, f.Severity)) ||
		!slices.Contains([]string{"all", "24h", "7d", "30d"}, f.Range) || len(f.Service) > 100 || len(f.Owner) > 100 || len(f.Search) > 100 {
		return invalid(errors.New("invalid saved view"))
	}
	return nil
}

func (s Store) ListSavedViews(ctx context.Context, actor, environment string) ([]SavedView, error) {
	if !slices.Contains([]string{"development", "staging"}, environment) {
		return nil, invalid(errors.New("invalid environment"))
	}
	rows, err := s.Pool.Query(ctx, `SELECT id,environment,name,filters,created_at,updated_at FROM incident.saved_views WHERE owner=$1 AND environment=$2 ORDER BY updated_at DESC,id DESC`, actor, environment)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SavedView{}
	for rows.Next() {
		var view SavedView
		var raw []byte
		if err := rows.Scan(&view.ID, &view.Environment, &view.Name, &raw, &view.CreatedAt, &view.UpdatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &view.Filters); err != nil {
			return nil, err
		}
		out = append(out, view)
	}
	return out, rows.Err()
}

func (s Store) CreateSavedView(ctx context.Context, actor string, input SavedViewInput) (SavedView, error) {
	var out SavedView
	if err := validateSavedView(&input); err != nil {
		return out, err
	}
	suffix, err := domain.NewID(12)
	if err != nil {
		return out, err
	}
	out.ID = "VIEW-" + suffix
	data, err := json.Marshal(input.Filters)
	if err != nil {
		return out, err
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer rollback(tx)
	// Serialize saves per operator so the 20-view limit also holds under concurrent requests.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, actor); err != nil {
		return out, err
	}
	var count int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM incident.saved_views WHERE owner=$1`, actor).Scan(&count); err != nil {
		return out, err
	}
	if count >= 20 {
		return out, invalid(errors.New("saved view limit reached (20)"))
	}
	err = tx.QueryRow(ctx, `INSERT INTO incident.saved_views(id,owner,environment,name,filters) VALUES($1,$2,$3,$4,$5) RETURNING created_at,updated_at`, out.ID, actor, input.Environment, input.Name, data).Scan(&out.CreatedAt, &out.UpdatedAt)
	if err != nil {
		return out, savedViewWriteError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		return out, err
	}
	out.Environment, out.Name, out.Filters = input.Environment, input.Name, input.Filters
	return out, nil
}

func (s Store) UpdateSavedView(ctx context.Context, actor, id string, input SavedViewInput) (SavedView, error) {
	var out SavedView
	if !savedViewID.MatchString(id) {
		return out, ErrNotFound
	}
	if err := validateSavedView(&input); err != nil {
		return out, err
	}
	data, err := json.Marshal(input.Filters)
	if err != nil {
		return out, err
	}
	out.ID, out.Environment, out.Name, out.Filters = id, input.Environment, input.Name, input.Filters
	err = s.Pool.QueryRow(ctx, `UPDATE incident.saved_views SET name=$3,filters=$4,updated_at=now() WHERE id=$1 AND owner=$2 AND environment=$5 RETURNING created_at,updated_at`, id, actor, input.Name, data, input.Environment).Scan(&out.CreatedAt, &out.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, ErrNotFound
	}
	return out, savedViewWriteError(err)
}

func (s Store) DeleteSavedView(ctx context.Context, actor, id string) error {
	if !savedViewID.MatchString(id) {
		return ErrNotFound
	}
	tag, err := s.Pool.Exec(ctx, `DELETE FROM incident.saved_views WHERE id=$1 AND owner=$2`, id, actor)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func savedViewWriteError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrSavedViewConflict
	}
	return err
}
