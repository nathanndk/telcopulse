package incident

import (
	"testing"
	"time"
)

func completeFields() Fields {
	return Fields{Title: "Payment failure", Severity: "SEV-2", Owner: "operator", RootCause: "Pool exhausted", Mitigation: "Restore capacity", Resolution: "Stable measurements", PostmortemNotes: "Limit concurrency"}
}
func recoveryAt(t time.Time) *RecoveryValidation {
	return &RecoveryValidation{Observation: "Business success rate returned to baseline after a synthetic purchase", SourceURL: "http://localhost:3001/transactions/recovery", ObservedAt: t}
}
func TestLifecycle(t *testing.T) {
	now := time.Now().UTC()
	current := Incident{State: Detected, Version: 1, Fields: completeFields()}
	for _, state := range []State{Acknowledged, Investigating, Identified, Mitigating, Monitoring, Resolved, Investigating, Identified, Mitigating, Monitoring, Resolved, Postmortem} {
		before := current
		var err error
		change := Update{Fields: completeFields(), State: state, Note: "Evidence reviewed"}
		if before.State == Monitoring && state == Resolved {
			change.RecoveryValidation = recoveryAt(now)
		}
		current, err = Apply(current, change, now)
		if err != nil {
			t.Fatalf("%s -> %s: %v", before.State, state, err)
		}
		if current.Version != before.Version+1 {
			t.Fatal("revision did not advance")
		}
		if state == Resolved && current.ResolvedAt == nil {
			t.Fatal("missing resolution time")
		}
		if state == Resolved && current.RecoveryValidation == nil {
			t.Fatal("missing recovery validation")
		}
		if before.State == Resolved && state == Investigating && current.ResolvedAt != nil {
			t.Fatal("reopening retained current resolution time")
		}
		if before.State == Resolved && state == Investigating && current.RecoveryValidation != nil {
			t.Fatal("reopening retained recovery validation")
		}
	}
	if current.AcknowledgedAt == nil {
		t.Fatal("missing acknowledgement time")
	}
}
func TestInvalidTransitionsAndGates(t *testing.T) {
	tests := []struct {
		name     string
		from, to State
		change   func(*Fields)
	}{
		{"skip acknowledgement", Detected, Resolved, func(*Fields) {}},
		{"unknown", Detected, "Closed", func(*Fields) {}},
		{"missing owner", Detected, Acknowledged, func(f *Fields) { f.Owner = "" }},
		{"missing cause", Investigating, Identified, func(f *Fields) { f.RootCause = "" }},
		{"missing mitigation", Identified, Mitigating, func(f *Fields) { f.Mitigation = "" }},
		{"missing resolution", Monitoring, Resolved, func(f *Fields) { f.Resolution = "" }},
		{"missing postmortem", Resolved, Postmortem, func(f *Fields) { f.PostmortemNotes = "" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := completeFields()
			tt.change(&f)
			_, err := Apply(Incident{State: tt.from, Version: 1}, Update{Fields: f, State: tt.to, Note: "investigation"}, time.Now())
			if err == nil {
				t.Fatal("invalid transition accepted")
			}
		})
	}
}

func TestResolutionRequiresCurrentSourcedRecovery(t *testing.T) {
	now := time.Now().UTC()
	current := Incident{State: Monitoring, Version: 6, DetectedAt: now.Add(-time.Hour), Fields: completeFields()}
	change := Update{Fields: completeFields(), State: Resolved, Note: "Recovery reviewed"}
	if _, err := Apply(current, change, now); err == nil {
		t.Fatal("resolution without recovery accepted")
	}
	for _, invalid := range []*RecoveryValidation{
		{Observation: "Purchase succeeded", SourceURL: "file:///tmp/proof", ObservedAt: now},
		{Observation: "Purchase succeeded", SourceURL: "https://user:pass@example.com", ObservedAt: now},
		{Observation: "Purchase succeeded", SourceURL: "https://example.com", ObservedAt: now.Add(-2 * time.Hour)},
		{Observation: "Purchase succeeded", SourceURL: "https://example.com", ObservedAt: now.Add(2 * time.Minute)},
	} {
		change.RecoveryValidation = invalid
		if _, err := Apply(current, change, now); err == nil {
			t.Fatalf("invalid recovery accepted: %+v", invalid)
		}
	}
	change.RecoveryValidation = recoveryAt(now)
	change.RecoveryValidation.ValidatedBy = "spoofed"
	change.RecoveryValidation.ValidatedAt = now.Add(-time.Hour)
	next, err := Apply(current, change, now)
	if err != nil || next.RecoveryValidation.ValidatedBy != "" || !next.RecoveryValidation.ValidatedAt.Equal(now) {
		t.Fatalf("recovery metadata not server controlled: %+v %v", next.RecoveryValidation, err)
	}
	change.State = Monitoring
	if _, err := Apply(current, change, now); err == nil {
		t.Fatal("recovery accepted outside resolution")
	}
}

func TestActionItemLegacyUpgradeAndStatusValidation(t *testing.T) {
	legacy := []ActionItem{
		{Title: "Restore payment capacity", Owner: "platform", Done: false},
		{Title: "Review pool settings", Owner: "database", Done: true},
	}
	normalized, err := normalizeActionItems(legacy)
	if err != nil || normalized[0].Status != "Open" || normalized[1].Status != "Completed" ||
		normalized[0].Priority != "P2" || normalized[1].Priority != "P2" ||
		legacy[0].Status != "" {
		t.Fatalf("legacy normalization changed source or returned wrong defaults: %+v %v", normalized, err)
	}
	fields := completeFields()
	fields.ActionItems = []ActionItem{{Title: "Track database recovery", Owner: "database", Priority: "P1", Status: "In Progress"}}
	next, err := Apply(Incident{State: Investigating, Version: 1}, Update{Fields: fields, State: Investigating, Note: "Action state changed"}, time.Now().UTC())
	if err != nil || next.ActionItems[0].Status != "In Progress" || next.ActionItems[0].Priority != "P1" || next.ActionItems[0].Done {
		t.Fatalf("structured action not persisted: %+v %v", next.ActionItems, err)
	}
	for _, item := range []ActionItem{
		{Title: "Invalid priority", Owner: "team", Priority: "P0", Status: "Open"},
		{Title: "Invalid status", Owner: "team", Priority: "P1", Status: "Waiting"},
		{Title: "Conflicting done", Owner: "team", Priority: "P2", Status: "Blocked", Done: true},
	} {
		fields.ActionItems = []ActionItem{item}
		if err := validateFields(fields); err == nil {
			t.Fatalf("invalid action accepted: %+v", item)
		}
	}
}
