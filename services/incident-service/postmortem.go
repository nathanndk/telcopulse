package incident

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"strings"
	"time"
)

// GeneratePostmortem supplies the retrospective narrative. Incident evidence,
// impact, actions and audited timeline are captured by the service itself.
type GeneratePostmortem struct {
	ExpectedVersion     int64  `json:"expected_version"`
	Summary             string `json:"summary"`
	Detection           string `json:"detection"`
	ContributingFactors string `json:"contributing_factors"`
	WhatWentWell        string `json:"what_went_well"`
	WhatWentWrong       string `json:"what_went_wrong"`
	Note                string `json:"note"`
}

type PostmortemMoment struct {
	Version int64     `json:"version"`
	Action  string    `json:"action"`
	State   State     `json:"state"`
	Actor   string    `json:"actor"`
	Note    string    `json:"note"`
	At      time.Time `json:"at"`
}

// PostmortemReport is an immutable source-version snapshot, not a live view.
type PostmortemReport struct {
	IncidentID          string              `json:"incident_id"`
	IncidentVersion     int64               `json:"incident_version"`
	GeneratedAt         time.Time           `json:"generated_at"`
	GeneratedBy         string              `json:"generated_by"`
	Summary             string              `json:"summary"`
	Impact              string              `json:"impact"`
	Detection           string              `json:"detection"`
	Timeline            []PostmortemMoment  `json:"timeline"`
	RootCause           string              `json:"root_cause"`
	ContributingFactors string              `json:"contributing_factors"`
	Mitigation          string              `json:"mitigation,omitempty"`
	Resolution          string              `json:"resolution"`
	RecoveryValidation  *RecoveryValidation `json:"recovery_validation,omitempty"`
	WhatWentWell        string              `json:"what_went_well"`
	WhatWentWrong       string              `json:"what_went_wrong"`
	ActionItems         []ActionItem        `json:"action_items"`
}

func validatePostmortem(input GeneratePostmortem) error {
	for _, value := range []string{input.Summary, input.Detection, input.ContributingFactors, input.WhatWentWell, input.WhatWentWrong} {
		if !bounded(value, 1, 4000) {
			return errors.New("all postmortem narrative sections are required and limited to 4000 characters")
		}
	}
	if !bounded(input.Note, 1, 2000) {
		return errors.New("an audit note is required")
	}
	return nil
}

// GeneratePostmortemAs atomically closes retrospective work and stores its
// immutable document alongside the incident's versioned audit transition.
func (s Store) GeneratePostmortemAs(ctx context.Context, id, actor, role string, input GeneratePostmortem) (PostmortemReport, error) {
	var report PostmortemReport
	if role != "" && role != "Incident Commander" && role != "Administrator" {
		return report, ErrForbidden
	}
	if input.ExpectedVersion < 1 || !bounded(actor, 1, 100) {
		return report, invalid(errors.New("actor and expected_version are required"))
	}
	if err := validatePostmortem(input); err != nil {
		return report, invalid(err)
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return report, err
	}
	defer rollback(tx)
	var data []byte
	err = tx.QueryRow(ctx, `SELECT document FROM incident.records WHERE id=$1 FOR UPDATE`, id).Scan(&data)
	if errors.Is(err, pgx.ErrNoRows) {
		return report, ErrNotFound
	}
	if err != nil {
		return report, err
	}
	var current Incident
	if err = json.Unmarshal(data, &current); err != nil {
		return report, err
	}
	if current.Version != input.ExpectedVersion {
		return report, ErrConflict
	}
	if current.State != Resolved && current.State != Postmortem {
		return report, invalid(errors.New("resolve the incident before generating a postmortem"))
	}
	var exists bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM incident.postmortems WHERE incident_id=$1)`, id).Scan(&exists); err != nil {
		return report, err
	}
	if exists {
		return report, ErrConflict
	}
	if !bounded(current.Impact, 1, 4000) {
		return report, invalid(errors.New("record impact before generating a postmortem"))
	}
	rows, err := tx.Query(ctx, `SELECT entry FROM incident.audit WHERE incident_id=$1 ORDER BY version`, id)
	if err != nil {
		return report, err
	}
	var timeline []PostmortemMoment
	for rows.Next() {
		var raw []byte
		var audit Audit
		if err = rows.Scan(&raw); err == nil {
			err = json.Unmarshal(raw, &audit)
		}
		if err != nil {
			break
		}
		timeline = append(timeline, PostmortemMoment{Version: audit.Version, Action: audit.Action, State: audit.After.State, Actor: audit.Actor, Note: audit.Note, At: audit.At})
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return report, err
	}
	if len(timeline) == 0 {
		return report, invalid(errors.New("incident timeline is missing"))
	}
	fields := current.Fields
	fields.PostmortemNotes = strings.TrimSpace(input.Summary)
	next, err := Apply(current, Update{Fields: fields, State: Postmortem, Note: input.Note}, time.Now().UTC())
	if err != nil {
		return report, invalid(err)
	}
	items, err := normalizeActionItems(current.ActionItems)
	if err != nil {
		return report, invalid(err)
	}
	timeline = append(timeline, PostmortemMoment{Version: next.Version, Action: "postmortem_generated", State: Postmortem, Actor: actor, Note: input.Note, At: next.UpdatedAt})
	report = PostmortemReport{
		IncidentID: id, IncidentVersion: next.Version, GeneratedAt: next.UpdatedAt, GeneratedBy: actor,
		Summary: fields.PostmortemNotes, Impact: current.Impact, Detection: strings.TrimSpace(input.Detection),
		Timeline: timeline, RootCause: current.RootCause, ContributingFactors: strings.TrimSpace(input.ContributingFactors), Mitigation: current.Mitigation,
		Resolution: current.Resolution, RecoveryValidation: current.RecoveryValidation, WhatWentWell: strings.TrimSpace(input.WhatWentWell),
		WhatWentWrong: strings.TrimSpace(input.WhatWentWrong), ActionItems: items,
	}
	reportData, err := json.Marshal(report)
	if err != nil {
		return report, err
	}
	nextData, err := json.Marshal(next)
	if err != nil {
		return report, err
	}
	if _, err = tx.Exec(ctx, `UPDATE incident.records SET document=$2,version=$3,state=$4,updated_at=$5 WHERE id=$1`, id, nextData, next.Version, next.State, next.UpdatedAt); err != nil {
		return report, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO incident.postmortems(incident_id,incident_version,document,generated_at) VALUES($1,$2,$3,$4)`, id, next.Version, reportData, next.UpdatedAt); err != nil {
		return report, err
	}
	if err = appendAudit(ctx, tx, Audit{Version: next.Version, Action: "postmortem_generated", Actor: actor, Note: input.Note, At: next.UpdatedAt, Before: &current, After: next}); err != nil {
		return report, err
	}
	return report, tx.Commit(ctx)
}
