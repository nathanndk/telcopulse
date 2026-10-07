package domain

import (
	"math"
	"testing"
	"time"
)

func TestPurchaseSLOBudget(t *testing.T) {
	now := time.Now()
	empty := NewPurchaseSLO(now.Add(-30*24*time.Hour), now, 0, 0, 0)
	if empty.CurrentPercent != nil || empty.BudgetConsumedPercent != nil || empty.BudgetRemaining != 0 {
		t.Fatalf("idle traffic was presented as healthy: %+v", empty)
	}
	healthy := NewPurchaseSLO(now.Add(-30*24*time.Hour), now, 9999, 1, 0)
	if healthy.CurrentPercent == nil || *healthy.CurrentPercent != 99.99 ||
		math.Abs(healthy.AllowedFailures-10) > 1e-9 ||
		math.Abs(healthy.BudgetRemaining-9) > 1e-9 ||
		healthy.BudgetConsumedPercent == nil || math.Abs(*healthy.BudgetConsumedPercent-10) > 1e-9 {
		t.Fatalf("incorrect healthy budget: %+v", healthy)
	}
	atLimit := NewPurchaseSLO(now.Add(-30*24*time.Hour), now, 999, 1, 0)
	if atLimit.BudgetRemaining != 0 || atLimit.BudgetConsumedPercent == nil || *atLimit.BudgetConsumedPercent != 100 {
		t.Fatalf("exact budget threshold: %+v", atLimit)
	}
	exhausted := NewPurchaseSLO(now.Add(-30*24*time.Hour), now, 998, 2, 0)
	if exhausted.BudgetRemaining != 0 || exhausted.BudgetConsumedPercent == nil || math.Abs(*exhausted.BudgetConsumedPercent-200) > 1e-9 {
		t.Fatalf("budget burn was capped or incorrect: %+v", exhausted)
	}
	declines := NewPurchaseSLO(now.Add(-30*24*time.Hour), now, 900, 1, 99)
	if declines.Failed != 100 || declines.TechnicalFailed != 1 || declines.BusinessDeclined != 99 ||
		declines.CurrentPercent == nil || *declines.CurrentPercent != 99.9 ||
		declines.BudgetConsumedPercent == nil || math.Abs(*declines.BudgetConsumedPercent-100) > 1e-9 {
		t.Fatalf("business declines must not burn the budget: %+v", declines)
	}
}

func TestPurchaseLatencySLOBudget(t *testing.T) {
	now := time.Now()
	if idle := NewPurchaseLatencySLO(now.Add(-30*24*time.Hour), now, 0, 0); idle.CurrentPercent != nil || idle.BudgetConsumedPercent != nil || idle.ThresholdMS != 2000 {
		t.Fatalf("idle latency presented as healthy: %+v", idle)
	}
	healthy := NewPurchaseLatencySLO(now, now, 1000, 4)
	if *healthy.CurrentPercent != 99.6 || healthy.AllowedSlow != 10 || healthy.BudgetRemaining != 6 || math.Abs(*healthy.BudgetConsumedPercent-40) > 1e-9 {
		t.Fatalf("latency budget: %+v", healthy)
	}
	breached := NewPurchaseLatencySLO(now, now, 200, 5)
	if breached.BudgetRemaining != 0 || math.Abs(*breached.BudgetConsumedPercent-250) > 1e-9 {
		t.Fatalf("latency burn capped: %+v", breached)
	}
}
