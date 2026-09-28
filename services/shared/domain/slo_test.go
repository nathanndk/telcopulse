package domain

import (
	"math"
	"testing"
	"time"
)

func TestPurchaseSLOBudget(t *testing.T) {
	now := time.Now()
	empty := NewPurchaseSLO(now.Add(-30*24*time.Hour), now, 0, 0)
	if empty.CurrentPercent != nil || empty.BudgetConsumedPercent != nil || empty.BudgetRemaining != 0 {
		t.Fatalf("idle traffic was presented as healthy: %+v", empty)
	}
	healthy := NewPurchaseSLO(now.Add(-30*24*time.Hour), now, 9999, 1)
	if healthy.CurrentPercent == nil || *healthy.CurrentPercent != 99.99 ||
		math.Abs(healthy.AllowedFailures-10) > 1e-9 ||
		math.Abs(healthy.BudgetRemaining-9) > 1e-9 ||
		healthy.BudgetConsumedPercent == nil || math.Abs(*healthy.BudgetConsumedPercent-10) > 1e-9 {
		t.Fatalf("incorrect healthy budget: %+v", healthy)
	}
	atLimit := NewPurchaseSLO(now.Add(-30*24*time.Hour), now, 999, 1)
	if atLimit.BudgetRemaining != 0 || atLimit.BudgetConsumedPercent == nil || *atLimit.BudgetConsumedPercent != 100 {
		t.Fatalf("exact budget threshold: %+v", atLimit)
	}
	exhausted := NewPurchaseSLO(now.Add(-30*24*time.Hour), now, 998, 2)
	if exhausted.BudgetRemaining != 0 || exhausted.BudgetConsumedPercent == nil || math.Abs(*exhausted.BudgetConsumedPercent-200) > 1e-9 {
		t.Fatalf("budget burn was capped or incorrect: %+v", exhausted)
	}
}
