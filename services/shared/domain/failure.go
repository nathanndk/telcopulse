package domain

import "slices"

// Failure classes label terminal purchase outcomes for objectives and alerts.
const (
	FailureNone      = "none"
	FailureTechnical = "technical"
	FailureBusiness  = "business"
)

// businessDeclineCodes are expected rule-based refusals: the platform worked
// correctly and the customer or catalog rules said no. Every other error code,
// including injected faults, is a technical failure.
var businessDeclineCodes = []string{"INSUFFICIENT_BALANCE"}

// BusinessDeclineCodes returns a copy of the error codes classified as business declines.
func BusinessDeclineCodes() []string { return slices.Clone(businessDeclineCodes) }

// FailureClass classifies a terminal outcome. Unknown or empty codes on a failed
// outcome are technical so that unclassified failures still burn the error budget.
func FailureClass(status, errorCode string) string {
	switch {
	case status != "FAILED":
		return FailureNone
	case slices.Contains(businessDeclineCodes, errorCode):
		return FailureBusiness
	default:
		return FailureTechnical
	}
}
