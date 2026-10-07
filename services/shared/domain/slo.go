package domain

import "time"

const PurchaseSuccessTargetPercent = 99.9
const purchasesPerAllowedFailure = 1000

// PurchaseSLO counts durable synthetic purchase outcomes in a rolling 30-day window.
// Technical failures, including injected faults, burn the error budget. Business
// declines such as insufficient balance are the platform working correctly: they
// count as good for the SLI and are reported separately.
type PurchaseSLO struct {
	WindowStart           time.Time `json:"window_start"`
	WindowEnd             time.Time `json:"window_end"`
	TargetPercent         float64   `json:"target_percent"`
	Total                 int64     `json:"total"`
	Success               int64     `json:"success"`
	Failed                int64     `json:"failed"`
	TechnicalFailed       int64     `json:"technical_failed"`
	BusinessDeclined      int64     `json:"business_declined"`
	CurrentPercent        *float64  `json:"current_percent"`
	AllowedFailures       float64   `json:"allowed_failures"`
	BudgetRemaining       float64   `json:"budget_remaining"`
	BudgetConsumedPercent *float64  `json:"budget_consumed_percent"`
}

func NewPurchaseSLO(start, end time.Time, success, technicalFailed, businessDeclined int64) PurchaseSLO {
	total := success + technicalFailed + businessDeclined
	out := PurchaseSLO{
		WindowStart: start, WindowEnd: end,
		TargetPercent: PurchaseSuccessTargetPercent,
		Total:         total, Success: success,
		Failed: technicalFailed + businessDeclined, TechnicalFailed: technicalFailed, BusinessDeclined: businessDeclined,
	}
	if total == 0 {
		return out
	}
	current := 100 * float64(total-technicalFailed) / float64(total)
	allowed := float64(total) / purchasesPerAllowedFailure
	consumed := 100 * float64(technicalFailed) / allowed
	remaining := allowed - float64(technicalFailed)
	if remaining < 0 {
		remaining = 0
	}
	out.CurrentPercent = &current
	out.AllowedFailures = allowed
	out.BudgetRemaining = remaining
	out.BudgetConsumedPercent = &consumed
	return out
}
