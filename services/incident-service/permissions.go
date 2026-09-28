package incident

import (
	"reflect"
	"slices"
)

// authorizeUpdate compares the full replacement command with the locked row.
// Operators may advance investigation but cannot silently replace protected
// fields, remove evidence, resolve, or reopen an already resolved incident.
func authorizeUpdate(current Incident, input Update, role string) error {
	switch role {
	case "", "Administrator", "Incident Commander":
		return nil
	case "Operator":
		currentActions, currentErr := normalizeActionItems(current.ActionItems)
		inputActions, inputErr := normalizeActionItems(input.ActionItems)
		if current.State == Resolved || current.State == Postmortem || input.State == Resolved || input.State == Postmortem ||
			input.Title != current.Title || input.Severity != current.Severity ||
			input.Resolution != current.Resolution || input.PostmortemNotes != current.PostmortemNotes ||
			currentErr != nil || inputErr != nil ||
			!slices.EqualFunc(currentActions, inputActions, func(a, b ActionItem) bool { return reflect.DeepEqual(a, b) }) {
			return ErrForbidden
		}
		if len(input.Evidence) < len(current.Evidence) || !slices.Equal(current.Evidence, input.Evidence[:len(current.Evidence)]) {
			return ErrForbidden
		}
		return nil
	default:
		return ErrForbidden
	}
}
