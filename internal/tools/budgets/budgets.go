package budgets

import "fmt"

type Limits struct {
	Tool    Tool
	Context Context
}

type Tool struct {
	MaxSelections  int
	MaxInvocations int
	MaxCostUnits   int
}

type Context struct {
	MaxExpandedDefinitions int
	MaxCompactedResults    int
	MaxCompactedBytes      int
}

type Usage struct {
	Selections          int
	Invocations         int
	CostUnits           int
	ExpandedDefinitions int
	CompactedResults    int
	CompactedBytes      int
}

type Tracker struct {
	limits Limits
	usage  Usage
}

func NewTracker(limits Limits) *Tracker {
	return &Tracker{limits: limits}
}

func (tracker *Tracker) RecordSelection(cost int) error {
	if tracker.limits.Tool.MaxSelections > 0 && tracker.usage.Selections+1 > tracker.limits.Tool.MaxSelections {
		return fmt.Errorf("tool selection budget exceeded")
	}
	if tracker.limits.Tool.MaxCostUnits > 0 && tracker.usage.CostUnits+cost > tracker.limits.Tool.MaxCostUnits {
		return fmt.Errorf("tool cost budget exceeded")
	}
	tracker.usage.Selections++
	tracker.usage.CostUnits += cost
	return nil
}

func (tracker *Tracker) RecordInvocation(cost int) error {
	if tracker.limits.Tool.MaxInvocations > 0 && tracker.usage.Invocations+1 > tracker.limits.Tool.MaxInvocations {
		return fmt.Errorf("tool invocation budget exceeded")
	}
	if tracker.limits.Tool.MaxCostUnits > 0 && tracker.usage.CostUnits+cost > tracker.limits.Tool.MaxCostUnits {
		return fmt.Errorf("tool cost budget exceeded")
	}
	tracker.usage.Invocations++
	tracker.usage.CostUnits += cost
	return nil
}

func (tracker *Tracker) RecordExpansion() error {
	if tracker.limits.Context.MaxExpandedDefinitions > 0 && tracker.usage.ExpandedDefinitions+1 > tracker.limits.Context.MaxExpandedDefinitions {
		return fmt.Errorf("context expansion budget exceeded")
	}
	tracker.usage.ExpandedDefinitions++
	return nil
}

func (tracker *Tracker) RecordCompaction(bytes int) error {
	if tracker.limits.Context.MaxCompactedResults > 0 && tracker.usage.CompactedResults+1 > tracker.limits.Context.MaxCompactedResults {
		return fmt.Errorf("compacted result budget exceeded")
	}
	if tracker.limits.Context.MaxCompactedBytes > 0 && tracker.usage.CompactedBytes+bytes > tracker.limits.Context.MaxCompactedBytes {
		return fmt.Errorf("compacted payload budget exceeded")
	}
	tracker.usage.CompactedResults++
	tracker.usage.CompactedBytes += bytes
	return nil
}

func (tracker *Tracker) Usage() Usage {
	return tracker.usage
}
