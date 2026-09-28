// Package incident owns durable incident state and its auditable lifecycle.
package incident

import (
	"errors"
	"fmt"
	"math"
	"net/url"
	"slices"
	"strings"
	"time"
)

// State is one of the eight operator lifecycle stages.
type State string

const (
	Detected      State = "Detected"
	Acknowledged  State = "Acknowledged"
	Investigating State = "Investigating"
	Identified    State = "Identified"
	Mitigating    State = "Mitigating"
	Monitoring    State = "Monitoring"
	Resolved      State = "Resolved"
	Postmortem    State = "Postmortem"
)

var transitions = map[State][]State{
	Detected: {Acknowledged}, Acknowledged: {Investigating}, Investigating: {Identified},
	Identified: {Mitigating, Investigating}, Mitigating: {Monitoring, Investigating},
	Monitoring: {Resolved, Mitigating, Investigating}, Resolved: {Postmortem, Investigating}, Postmortem: {},
}

// Evidence references observations; the service never fetches user-supplied URLs.
type Evidence struct {
	Kind    string `json:"kind"`
	Summary string `json:"summary"`
	URL     string `json:"url,omitempty"`
}

// RecoveryValidation records the observation reviewed for a resolution. The
// actor and validation time are assigned by the service, never by the client.
type RecoveryValidation struct {
	Observation string    `json:"observation"`
	SourceURL   string    `json:"source_url"`
	ObservedAt  time.Time `json:"observed_at"`
	ValidatedBy string    `json:"validated_by,omitempty"`
	ValidatedAt time.Time `json:"validated_at,omitempty"`
}

// ActionItem is a named follow-up with accountable ownership.
type ActionItem struct {
	Title    string     `json:"title"`
	Owner    string     `json:"owner"`
	Priority string     `json:"priority,omitempty"`
	Status   string     `json:"status,omitempty"`
	Done     bool       `json:"done"`
	DueAt    *time.Time `json:"due_at,omitempty"`
}

// normalizeActionItems upgrades legacy done-only entries on the next write.
// Status is authoritative when supplied; Done remains a response and request
// compatibility field for older clients.
func normalizeActionItems(items []ActionItem) ([]ActionItem, error) {
	if items == nil {
		return nil, nil
	}
	out := make([]ActionItem, len(items))
	for index, item := range items {
		if item.Priority == "" {
			item.Priority = "P2"
		}
		if !slices.Contains([]string{"P1", "P2", "P3"}, item.Priority) {
			return nil, errors.New("invalid action priority")
		}
		if item.Status == "" {
			if item.Done {
				item.Status = "Completed"
			} else {
				item.Status = "Open"
			}
		}
		if !slices.Contains([]string{"Open", "In Progress", "Blocked", "Completed"}, item.Status) {
			return nil, errors.New("invalid action status")
		}
		if item.Status != "Completed" && item.Done {
			return nil, errors.New("action done conflicts with status")
		}
		item.Done = item.Status == "Completed"
		out[index] = item
	}
	return out, nil
}

// Fields are operator-maintained details. Nil measurements mean unknown, not zero.
type Fields struct {
	Title                string       `json:"title"`
	Severity             string       `json:"severity"`
	Owner                string       `json:"owner"`
	Impact               string       `json:"impact"`
	RootCause            string       `json:"root_cause"`
	Mitigation           string       `json:"mitigation"`
	Resolution           string       `json:"resolution"`
	PostmortemNotes      string       `json:"postmortem_notes"`
	RelatedDeployment    string       `json:"related_deployment"`
	AffectedTransactions *int64       `json:"affected_transactions"`
	AffectedUsers        *int64       `json:"affected_users"`
	ErrorRate            *float64     `json:"error_rate"`
	SuccessRate          *float64     `json:"success_rate"`
	LatencyMS            *float64     `json:"latency_ms"`
	Evidence             []Evidence   `json:"evidence"`
	ActionItems          []ActionItem `json:"action_items"`
}

// Create is the immutable detection envelope plus initial editable details.
type Create struct {
	Fields
	Environment string     `json:"environment"`
	Service     string     `json:"service"`
	DetectedAt  *time.Time `json:"detected_at,omitempty"`
}

// Incident is a versioned snapshot. Detection identity cannot change during edits.
type Incident struct {
	Fields
	ID                 string              `json:"id"`
	Environment        string              `json:"environment"`
	Service            string              `json:"service"`
	OwningTeam         string              `json:"owning_team"`
	EscalationLevel    int                 `json:"escalation_level"`
	EscalatedAt        *time.Time          `json:"escalated_at"`
	State              State               `json:"state"`
	Version            int64               `json:"version"`
	CreatedAt          time.Time           `json:"created_at"`
	DetectedAt         time.Time           `json:"detected_at"`
	UpdatedAt          time.Time           `json:"updated_at"`
	AcknowledgedAt     *time.Time          `json:"acknowledged_at"`
	ResolvedAt         *time.Time          `json:"resolved_at"`
	RecoveryValidation *RecoveryValidation `json:"recovery_validation,omitempty"`
}

// Update replaces editable fields and optionally advances the state at a known version.
type Update struct {
	Fields
	State              State               `json:"state"`
	ExpectedVersion    int64               `json:"expected_version"`
	Note               string              `json:"note"`
	RecoveryValidation *RecoveryValidation `json:"recovery_validation,omitempty"`
}

// Escalate transfers coordination to a named team at the current revision.
type Escalate struct {
	ExpectedVersion int64  `json:"expected_version"`
	Team            string `json:"team"`
	Owner           string `json:"owner"`
	Severity        string `json:"severity"`
	Reason          string `json:"reason"`
}

// Audit records exact before/after snapshots in the same transaction as a mutation.
type Audit struct {
	Version int64     `json:"version"`
	Action  string    `json:"action,omitempty"`
	Actor   string    `json:"actor"`
	Note    string    `json:"note"`
	At      time.Time `json:"at"`
	Before  *Incident `json:"before"`
	After   Incident  `json:"after"`
}

// Detail returns a current snapshot and ordered lifecycle/edit history.
type Detail struct {
	Incident    Incident          `json:"incident"`
	Postmortem  *PostmortemReport `json:"postmortem,omitempty"`
	History     []Audit           `json:"history"`
	HistoryMore bool              `json:"history_more"`
	HistoryNext int64             `json:"history_next,omitempty"`
}

func bounded(value string, min, max int) bool {
	return len(strings.TrimSpace(value)) >= min && len(value) <= max
}

func validateRecovery(v *RecoveryValidation, detectedAt, now time.Time) error {
	if v == nil || !bounded(v.Observation, 10, 1000) || len(v.SourceURL) > 2048 || v.ObservedAt.IsZero() {
		return errors.New("recovery observation, source URL and observation time are required")
	}
	u, err := url.Parse(v.SourceURL)
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return errors.New("recovery source URL must be HTTP(S) without credentials")
	}
	if v.ObservedAt.After(now.Add(time.Minute)) || (!detectedAt.IsZero() && v.ObservedAt.Before(detectedAt)) {
		return errors.New("recovery observation must follow detection and cannot be in the future")
	}
	return nil
}
func validateFields(f Fields) error {
	if !bounded(f.Title, 3, 200) || !slices.Contains([]string{"SEV-1", "SEV-2", "SEV-3", "SEV-4"}, f.Severity) {
		return errors.New("title and severity are required")
	}
	if len(f.Owner) > 100 || len(f.RelatedDeployment) > 200 {
		return errors.New("owner or deployment is too long")
	}
	for _, s := range []string{f.Impact, f.RootCause, f.Mitigation, f.Resolution, f.PostmortemNotes} {
		if len(s) > 4000 {
			return errors.New("incident detail exceeds 4000 characters")
		}
	}
	for _, n := range []*int64{f.AffectedTransactions, f.AffectedUsers} {
		if n != nil && *n < 0 {
			return errors.New("impact counts must be nonnegative")
		}
	}
	for _, n := range []*float64{f.ErrorRate, f.SuccessRate} {
		if n != nil && (*n < 0 || *n > 1 || math.IsNaN(*n) || math.IsInf(*n, 0)) {
			return errors.New("rates must be between zero and one")
		}
	}
	if f.LatencyMS != nil && (*f.LatencyMS < 0 || math.IsNaN(*f.LatencyMS) || math.IsInf(*f.LatencyMS, 0)) {
		return errors.New("latency must be nonnegative")
	}
	if len(f.Evidence) > 50 || len(f.ActionItems) > 50 {
		return errors.New("too many evidence or action items")
	}
	for _, e := range f.Evidence {
		if !slices.Contains([]string{"metric", "log", "trace", "deployment", "note"}, e.Kind) || !bounded(e.Summary, 1, 1000) || len(e.URL) > 2048 {
			return errors.New("invalid evidence")
		}
		if e.URL != "" {
			u, err := url.Parse(e.URL)
			if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
				return errors.New("evidence URL must be HTTP(S) without credentials")
			}
		}
	}
	for _, a := range f.ActionItems {
		if !bounded(a.Title, 1, 500) || !bounded(a.Owner, 1, 100) {
			return errors.New("action items require a title and owner")
		}
	}
	if _, err := normalizeActionItems(f.ActionItems); err != nil {
		return err
	}
	return nil
}

// Apply validates lifecycle gates and computes timestamps without persistence effects.
func Apply(current Incident, change Update, now time.Time) (Incident, error) {
	if err := validateFields(change.Fields); err != nil {
		return current, err
	}
	if _, ok := transitions[change.State]; !ok {
		return current, errors.New("unknown incident state")
	}
	if current.State != change.State && !slices.Contains(transitions[current.State], change.State) {
		return current, fmt.Errorf("cannot transition from %s to %s", current.State, change.State)
	}
	if !bounded(change.Note, 1, 2000) {
		return current, errors.New("an audit note is required")
	}
	if change.State != Detected && !bounded(change.Owner, 1, 100) {
		return current, errors.New("an owner is required after detection")
	}
	if slices.Contains([]State{Identified, Mitigating, Monitoring, Resolved, Postmortem}, change.State) && !bounded(change.RootCause, 1, 4000) {
		return current, errors.New("root cause is required after identification")
	}
	if slices.Contains([]State{Mitigating, Monitoring, Resolved, Postmortem}, change.State) && !bounded(change.Mitigation, 1, 4000) {
		return current, errors.New("mitigation is required")
	}
	if slices.Contains([]State{Resolved, Postmortem}, change.State) && !bounded(change.Resolution, 1, 4000) {
		return current, errors.New("resolution is required")
	}
	if current.State == Monitoring && change.State == Resolved {
		if err := validateRecovery(change.RecoveryValidation, current.DetectedAt, now); err != nil {
			return current, err
		}
	} else if change.RecoveryValidation != nil {
		return current, errors.New("recovery validation is accepted only when resolving from Monitoring")
	}
	if change.State == Postmortem && !bounded(change.PostmortemNotes, 1, 4000) {
		return current, errors.New("postmortem notes are required")
	}
	change.ActionItems, _ = normalizeActionItems(change.ActionItems)
	current.Fields = change.Fields
	if current.AcknowledgedAt == nil && change.State == Acknowledged {
		current.AcknowledgedAt = &now
	}
	if current.State != Resolved && change.State == Resolved {
		current.ResolvedAt = &now
		validation := *change.RecoveryValidation
		validation.ValidatedAt = now
		validation.ValidatedBy = ""
		current.RecoveryValidation = &validation
	}
	if current.State == Resolved && change.State == Investigating {
		current.ResolvedAt = nil
		current.RecoveryValidation = nil
	}
	current.State = change.State
	current.UpdatedAt = now
	current.Version++
	return current, nil
}

// Summary is the bounded list projection; investigation details are read separately.
type Summary struct {
	ID                   string    `json:"id"`
	Title                string    `json:"title"`
	Severity             string    `json:"severity"`
	Owner                string    `json:"owner"`
	OwningTeam           string    `json:"owning_team"`
	EscalationLevel      int       `json:"escalation_level"`
	Environment          string    `json:"environment"`
	Service              string    `json:"service"`
	State                State     `json:"state"`
	Version              int64     `json:"version"`
	CreatedAt            time.Time `json:"created_at"`
	DetectedAt           time.Time `json:"detected_at"`
	UpdatedAt            time.Time `json:"updated_at"`
	AffectedTransactions *int64    `json:"affected_transactions"`
	AffectedUsers        *int64    `json:"affected_users"`
}
