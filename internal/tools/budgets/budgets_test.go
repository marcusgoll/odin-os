package budgets

import "testing"

func TestTrackerEnforcesToolBudget(t *testing.T) {
	t.Parallel()

	tracker := NewTracker(Limits{
		Tool: Tool{
			MaxSelections:  1,
			MaxInvocations: 1,
			MaxCostUnits:   2,
		},
	})

	if err := tracker.RecordSelection(1); err != nil {
		t.Fatalf("RecordSelection() error = %v", err)
	}
	if err := tracker.RecordInvocation(1); err != nil {
		t.Fatalf("RecordInvocation() error = %v", err)
	}
	if err := tracker.RecordSelection(1); err == nil {
		t.Fatalf("RecordSelection() error = nil, want budget error")
	}
}

func TestTrackerEnforcesContextBudget(t *testing.T) {
	t.Parallel()

	tracker := NewTracker(Limits{
		Context: Context{
			MaxExpandedDefinitions: 1,
			MaxCompactedResults:    1,
			MaxCompactedBytes:      20,
		},
	})

	if err := tracker.RecordExpansion(); err != nil {
		t.Fatalf("RecordExpansion() error = %v", err)
	}
	if err := tracker.RecordCompaction(10); err != nil {
		t.Fatalf("RecordCompaction() error = %v", err)
	}
	if err := tracker.RecordExpansion(); err == nil {
		t.Fatalf("RecordExpansion() error = nil, want budget error")
	}
}

func TestSchedulerBudgetRespectsConcurrencyAndCycleCaps(t *testing.T) {
	t.Parallel()

	budget := SchedulerBudget{
		MaxConcurrentRuns: 1,
		MaxStartsPerCycle: 1,
	}

	if !budget.CanStart(0, 0) {
		t.Fatal("CanStart(0, 0) = false, want true")
	}
	if budget.CanStart(1, 0) {
		t.Fatal("CanStart(1, 0) = true, want false")
	}
	if budget.CanStart(0, 1) {
		t.Fatal("CanStart(0, 1) = true, want false")
	}
}
