package domain

import "time"

const PurchaseSuccessTargetPercent = 99.9
const purchasesPerAllowedFailure = 1000

// PurchaseSLO counts durable synthetic purchase outcomes in a rolling 30-day window.
// Failed business outcomes count, including intentional declines and simulations.
type PurchaseSLO struct {
	WindowStart           time.Time `json:"window_start"`
	WindowEnd             time.Time `json:"window_end"`
	TargetPercent         float64   `json:"target_percent"`
	Total                 int64     `json:"total"`
	Success               int64     `json:"success"`
	Failed                int64     `json:"failed"`
	CurrentPercent        *float64  `json:"current_percent"`
	AllowedFailures       float64   `json:"allowed_failures"`
	BudgetRemaining       float64   `json:"budget_remaining"`
	BudgetConsumedPercent *float64  `json:"budget_consumed_percent"`
}

func NewPurchaseSLO(start, end time.Time, success, failed int64) PurchaseSLO {
	total := success + failed
	out := PurchaseSLO{
		WindowStart: start, WindowEnd: end,
		TargetPercent: PurchaseSuccessTargetPercent,
		Total:         total, Success: success, Failed: failed,
	}
	if total == 0 {
		return out
	}
	current := 100 * float64(success) / float64(total)
	allowed := float64(total) / purchasesPerAllowedFailure
	consumed := 100 * float64(failed) / allowed
	remaining := allowed - float64(failed)
	if remaining < 0 {
		remaining = 0
	}
	out.CurrentPercent = &current
	out.AllowedFailures = allowed
	out.BudgetRemaining = remaining
	out.BudgetConsumedPercent = &consumed
	return out
}
