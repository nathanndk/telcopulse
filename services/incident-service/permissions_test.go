package incident

import (
	"errors"
	"testing"
)

func TestOperatorUpdatePolicy(t *testing.T) {
	current := Incident{Fields: Fields{Title: "Payment incident", Severity: "SEV-2", Owner: "on-call", Evidence: []Evidence{{Kind: "metric", Summary: "Initial"}}, ActionItems: []ActionItem{{Title: "Follow up", Owner: "team"}}}, State: Detected}
	change := Update{Fields: current.Fields, State: Acknowledged, ExpectedVersion: 1, Note: "Accepted by on-call"}
	change.Owner = "new-on-call"
	change.Evidence = append(append([]Evidence{}, current.Evidence...), Evidence{Kind: "trace", Summary: "Payment timeout"})
	if err := authorizeUpdate(current, change, "Operator"); err != nil {
		t.Fatalf("assignment, acknowledgement and evidence append denied: %v", err)
	}
	change.ActionItems = []ActionItem{{Title: "Follow up", Owner: "team", Priority: "P2", Status: "Open"}}
	if err := authorizeUpdate(current, change, "Operator"); err != nil {
		t.Fatalf("legacy action default changed operator permissions: %v", err)
	}
	for name, mutate := range map[string]func(*Update){
		"severity":         func(v *Update) { v.Severity = "SEV-1" },
		"title":            func(v *Update) { v.Title = "Different" },
		"resolve":          func(v *Update) { v.State = Resolved },
		"resolution":       func(v *Update) { v.Resolution = "Done" },
		"postmortem":       func(v *Update) { v.PostmortemNotes = "Done" },
		"action items":     func(v *Update) { v.ActionItems = nil },
		"evidence edit":    func(v *Update) { v.Evidence[0].Summary = "Replaced" },
		"evidence removal": func(v *Update) { v.Evidence = nil },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := change
			candidate.Evidence = append([]Evidence{}, change.Evidence...)
			mutate(&candidate)
			if err := authorizeUpdate(current, candidate, "Operator"); !errors.Is(err, ErrForbidden) {
				t.Fatalf("forbidden change accepted: %v", err)
			}
			if err := authorizeUpdate(current, candidate, "Incident Commander"); err != nil {
				t.Fatalf("commander denied: %v", err)
			}
		})
	}
	if err := authorizeUpdate(current, change, "Viewer"); !errors.Is(err, ErrForbidden) {
		t.Fatal("viewer update accepted")
	}
	current.State = Resolved
	if err := authorizeUpdate(current, change, "Operator"); !errors.Is(err, ErrForbidden) {
		t.Fatal("operator reopened resolved incident")
	}
}
