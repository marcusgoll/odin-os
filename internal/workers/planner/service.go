package planner

import (
	"fmt"

	"odin-os/internal/tools/broker"
	"odin-os/internal/tools/catalog"
)

type Service struct {
	Broker *broker.Broker
}

type PlanContext struct {
	Cards []catalog.Card
}

type Selection struct {
	Key              string
	InvokeTool       bool
	ToolInput        map[string]string
	AllowSubAgentUse bool
}

type ExecutionContext struct {
	Cards      []catalog.Card
	Expansions []catalog.Expansion
	Compacted  []catalog.CompactedResult
}

func (service Service) Prepare(scope string) (PlanContext, error) {
	if service.Broker == nil {
		return PlanContext{}, fmt.Errorf("planner broker is required")
	}
	return PlanContext{
		Cards: service.Broker.Catalog(scope),
	}, nil
}

func (service Service) Materialize(scope string, selections []Selection) (ExecutionContext, error) {
	if service.Broker == nil {
		return ExecutionContext{}, fmt.Errorf("planner broker is required")
	}

	context := ExecutionContext{
		Cards: service.Broker.Catalog(scope),
	}

	for _, selection := range selections {
		expansion, err := service.Broker.Expand(selection.Key)
		if err != nil {
			return ExecutionContext{}, err
		}
		if expansion.SubAgent != nil && !selection.AllowSubAgentUse {
			return ExecutionContext{}, fmt.Errorf("sub-agent expansion requires explicit plan opt-in")
		}

		context.Expansions = append(context.Expansions, expansion)

		if selection.InvokeTool {
			if expansion.Tool == nil {
				return ExecutionContext{}, fmt.Errorf("capability %q is not a tool", selection.Key)
			}
			result, err := service.Broker.InvokeTool(selection.Key, selection.ToolInput)
			if err != nil {
				return ExecutionContext{}, err
			}
			compacted, err := service.Broker.Compact(result)
			if err != nil {
				return ExecutionContext{}, err
			}
			context.Compacted = append(context.Compacted, compacted)
		}
	}

	return context, nil
}
