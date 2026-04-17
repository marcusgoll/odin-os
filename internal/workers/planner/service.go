package planner

import (
	"fmt"

	"odin-os/internal/tools/broker"
	"odin-os/internal/tools/catalog"
)

type Service struct {
	Broker *broker.Broker
}

type WorkspaceContext struct {
	Key string
}

type InitiativeContext struct {
	Key  string
	Kind string
}

type CompanionContext struct {
	Key                string
	Kind               string
	ToolPolicyJSON     string
	PlanningPolicyJSON string
}

type MemoryReference struct {
	Scope   string
	Summary string
	Ref     string
}

type PrepareInput struct {
	Scope            string
	Workspace        WorkspaceContext
	Initiative       *InitiativeContext
	Companion        *CompanionContext
	MemoryReferences []MemoryReference
}

type PlanContext struct {
	Scope            string
	Workspace        WorkspaceContext
	Initiative       *InitiativeContext
	Companion        *CompanionContext
	MemoryReferences []MemoryReference
	Cards            []catalog.Card
}

type Selection struct {
	Key               string
	InvokeTool        bool
	ToolInput         map[string]string
	AllowAgentRoleUse bool
}

type MaterializeInput struct {
	Scope            string
	Workspace        WorkspaceContext
	Initiative       *InitiativeContext
	Companion        *CompanionContext
	MemoryReferences []MemoryReference
	Selections       []Selection
}

type ExecutionContext struct {
	Scope            string
	Workspace        WorkspaceContext
	Initiative       *InitiativeContext
	Companion        *CompanionContext
	MemoryReferences []MemoryReference
	Cards            []catalog.Card
	Expansions       []catalog.Expansion
	Compacted        []catalog.CompactedResult
}

func (service Service) Prepare(input PrepareInput) (PlanContext, error) {
	if service.Broker == nil {
		return PlanContext{}, fmt.Errorf("planner broker is required")
	}

	return PlanContext{
		Scope:            input.Scope,
		Workspace:        input.Workspace,
		Initiative:       cloneInitiativeContext(input.Initiative),
		Companion:        cloneCompanionContext(input.Companion),
		MemoryReferences: cloneMemoryReferences(input.MemoryReferences),
		Cards:            service.Broker.Catalog(input.Scope),
	}, nil
}

func (service Service) Materialize(input MaterializeInput) (ExecutionContext, error) {
	if service.Broker == nil {
		return ExecutionContext{}, fmt.Errorf("planner broker is required")
	}

	context := ExecutionContext{
		Scope:            input.Scope,
		Workspace:        input.Workspace,
		Initiative:       cloneInitiativeContext(input.Initiative),
		Companion:        cloneCompanionContext(input.Companion),
		MemoryReferences: cloneMemoryReferences(input.MemoryReferences),
		Cards:            service.Broker.Catalog(input.Scope),
	}

	for _, selection := range input.Selections {
		expansion, err := service.Broker.Expand(selection.Key)
		if err != nil {
			return ExecutionContext{}, err
		}
		if expansion.AgentRole != nil && !selection.AllowAgentRoleUse {
			return ExecutionContext{}, fmt.Errorf("agent-role expansion requires explicit plan opt-in")
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

func cloneInitiativeContext(input *InitiativeContext) *InitiativeContext {
	if input == nil {
		return nil
	}
	cloned := *input
	return &cloned
}

func cloneCompanionContext(input *CompanionContext) *CompanionContext {
	if input == nil {
		return nil
	}
	cloned := *input
	return &cloned
}

func cloneMemoryReferences(input []MemoryReference) []MemoryReference {
	if len(input) == 0 {
		return nil
	}
	cloned := make([]MemoryReference, len(input))
	copy(cloned, input)
	return cloned
}
