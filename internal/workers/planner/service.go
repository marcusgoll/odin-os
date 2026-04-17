package planner

import (
	"context"
	"fmt"

	"odin-os/internal/skills"
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
	InvokeSkill      bool
	SkillInput       map[string]any
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
	cards, err := service.Broker.Catalog(scope)
	if err != nil {
		return PlanContext{}, err
	}
	return PlanContext{
		Cards: cards,
	}, nil
}

func (service Service) Materialize(ctx context.Context, scope string, invocationContext skills.InvocationContext, selections []Selection) (ExecutionContext, error) {
	if service.Broker == nil {
		return ExecutionContext{}, fmt.Errorf("planner broker is required")
	}

	cards, err := service.Broker.Catalog(scope)
	if err != nil {
		return ExecutionContext{}, err
	}
	context := ExecutionContext{
		Cards: cards,
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

		if selection.InvokeSkill {
			if expansion.Skill == nil {
				return ExecutionContext{}, fmt.Errorf("capability %q is not a skill", selection.Key)
			}
			result, err := service.Broker.InvokeSkill(ctx, skills.InvokeRequest{
				Key:     selection.Key,
				Input:   selection.SkillInput,
				Context: invocationContext,
			})
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
