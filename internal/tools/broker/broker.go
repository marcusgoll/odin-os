package broker

import (
	"context"
	"fmt"

	"odin-os/internal/registry"
	"odin-os/internal/skills"
	"odin-os/internal/tools/budgets"
	"odin-os/internal/tools/catalog"
)

type Broker struct {
	source       SnapshotSource
	builtins     map[string]catalog.ToolDefinition
	skillInvoker SkillInvoker
	tracker      *budgets.Tracker
}

type SkillInvoker interface {
	Invoke(context.Context, skills.InvokeRequest) (skills.InvokeResponse, error)
}

func New(source SnapshotSource, builtins map[string]catalog.ToolDefinition, skillInvoker SkillInvoker, limits budgets.Limits) *Broker {
	if source == nil {
		source = StaticSource(registry.Snapshot{})
	}
	return &Broker{
		source:       source,
		builtins:     builtins,
		skillInvoker: skillInvoker,
		tracker:      budgets.NewTracker(limits),
	}
}

func (broker *Broker) Catalog(scope string) ([]catalog.Card, error) {
	snapshot, err := broker.currentSnapshot()
	if err != nil {
		return nil, err
	}

	scope = catalog.NormalizeScope(scope)
	cards := make([]catalog.Card, 0, len(broker.builtins)+len(snapshot.Items))

	for _, definition := range broker.builtins {
		if catalog.MatchesScope(definition.Scopes, scope) {
			cards = append(cards, definition.Card())
		}
	}

	for _, item := range snapshot.Items {
		card, ok := catalog.CardFromRegistry(item)
		if !ok {
			continue
		}
		if catalog.MatchesScope(card.Scopes, scope) {
			cards = append(cards, card)
		}
	}

	catalog.SortCards(cards)
	return cards, nil
}

func (broker *Broker) Expand(key string) (catalog.Expansion, error) {
	if definition, ok := broker.builtins[key]; ok {
		if err := broker.tracker.RecordSelection(definition.BudgetCost); err != nil {
			return catalog.Expansion{}, err
		}
		if err := broker.tracker.RecordExpansion(); err != nil {
			return catalog.Expansion{}, err
		}
		toolCopy := definition
		return catalog.Expansion{
			Card: definition.Card(),
			Tool: &toolCopy,
		}, nil
	}

	snapshot, err := broker.currentSnapshot()
	if err != nil {
		return catalog.Expansion{}, err
	}

	item, ok := snapshot.ByKey[key]
	if !ok {
		return catalog.Expansion{}, fmt.Errorf("unknown capability %q", key)
	}

	card, ok := catalog.CardFromRegistry(item)
	if !ok {
		return catalog.Expansion{}, fmt.Errorf("capability %q is not broker-expandable", key)
	}
	if err := broker.tracker.RecordSelection(card.BudgetCost); err != nil {
		return catalog.Expansion{}, err
	}
	if err := broker.tracker.RecordExpansion(); err != nil {
		return catalog.Expansion{}, err
	}

	switch item.Kind {
	case registry.KindSkill:
		return catalog.Expansion{
			Card: card,
			Skill: &catalog.SkillDefinition{
				Key:            item.Key,
				Title:          item.Title,
				Summary:        item.Summary,
				Version:        item.Version,
				Enabled:        item.Enabled,
				Tags:           append([]string(nil), item.Tags...),
				Scopes:         append([]string(nil), item.Scopes...),
				AppliesTo:      append([]string(nil), item.AppliesTo...),
				Composes:       append([]string(nil), item.Composes...),
				Permissions:    append([]string(nil), item.Permissions...),
				HandlerType:    item.HandlerType,
				HandlerRef:     item.HandlerRef,
				TimeoutSeconds: item.TimeoutSeconds,
				InputSchema:    catalog.CloneAnyMap(item.LegacyInputSchema),
				OutputSchema:   catalog.CloneAnyMap(item.LegacyOutputSchema),
				Sections:       catalog.CloneSections(item.Sections),
				SourceRef:      item.Source.RelativePath,
			},
		}, nil
	case registry.KindAgent:
		return catalog.Expansion{
			Card: card,
			AgentRole: &catalog.AgentRoleDefinition{
				Key:       item.Key,
				Title:     item.Title,
				Summary:   item.Summary,
				Tags:      append([]string(nil), item.Tags...),
				Scopes:    append([]string(nil), item.Scopes...),
				AppliesTo: append([]string(nil), item.AppliesTo...),
				Composes:  append([]string(nil), item.Composes...),
				Tools:     append([]string(nil), item.Tools...),
				Role:      item.Role,
				Sections:  catalog.CloneSections(item.Sections),
				SourceRef: item.Source.RelativePath,
			},
		}, nil
	case registry.KindWorkflow:
		return catalog.Expansion{
			Card: card,
			Workflow: &catalog.WorkflowDefinition{
				Key:          item.Key,
				Title:        item.Title,
				Summary:      item.Summary,
				Version:      item.Version,
				Tags:         append([]string(nil), item.Tags...),
				Scopes:       append([]string(nil), item.Scopes...),
				AppliesTo:    append([]string(nil), item.AppliesTo...),
				Entrypoint:   item.Entrypoint,
				Composes:     append([]string(nil), item.Composes...),
				Dependencies: append([]registry.DependencyRef(nil), item.Dependencies...),
				Sections:     catalog.CloneSections(item.Sections),
				SourceRef:    item.Source.RelativePath,
			},
		}, nil
	case registry.KindCommand:
		return catalog.Expansion{
			Card: card,
			OperatorCommand: &catalog.OperatorCommandDefinition{
				Key:       item.Key,
				Title:     item.Title,
				Summary:   item.Summary,
				Tags:      append([]string(nil), item.Tags...),
				Scopes:    append([]string(nil), item.Scopes...),
				AppliesTo: append([]string(nil), item.AppliesTo...),
				Composes:  append([]string(nil), item.Composes...),
				Command:   item.Command,
				Aliases:   append([]string(nil), item.Aliases...),
				Sections:  catalog.CloneSections(item.Sections),
				SourceRef: item.Source.RelativePath,
			},
		}, nil
	default:
		return catalog.Expansion{}, fmt.Errorf("capability %q is not broker-expandable", key)
	}
}

func (broker *Broker) InvokeTool(key string, input map[string]string) (catalog.StructuredResult, error) {
	definition, ok := broker.builtins[key]
	if !ok {
		return catalog.StructuredResult{}, fmt.Errorf("unknown tool %q", key)
	}
	if err := broker.tracker.RecordInvocation(definition.BudgetCost); err != nil {
		return catalog.StructuredResult{}, err
	}
	if definition.Invoke == nil {
		return catalog.StructuredResult{}, fmt.Errorf("tool %q is not invokable", key)
	}
	result, err := definition.Invoke(input)
	if err != nil {
		return catalog.StructuredResult{}, err
	}
	return result, nil
}

func (broker *Broker) InvokeSkill(ctx context.Context, request skills.InvokeRequest) (catalog.StructuredResult, error) {
	if broker.skillInvoker == nil {
		return catalog.StructuredResult{}, fmt.Errorf("skill %q is not invokable", request.Key)
	}

	snapshot, err := broker.currentSnapshot()
	if err != nil {
		return catalog.StructuredResult{}, err
	}
	item, ok := snapshot.ByKey[request.Key]
	if !ok || item.Kind != registry.KindSkill {
		return catalog.StructuredResult{}, fmt.Errorf("unknown skill %q", request.Key)
	}

	card, ok := catalog.CardFromRegistry(item)
	if !ok {
		return catalog.StructuredResult{}, fmt.Errorf("capability %q is not invokable", request.Key)
	}
	if err := broker.tracker.RecordInvocation(card.BudgetCost); err != nil {
		return catalog.StructuredResult{}, err
	}

	request.Input = catalog.CloneAnyMap(request.Input)
	response, err := broker.skillInvoker.Invoke(ctx, request)
	if err != nil {
		return catalog.StructuredResult{}, err
	}

	return structuredResultFromSkillInvocation(response), nil
}

func (broker *Broker) Compact(result catalog.StructuredResult) (catalog.CompactedResult, error) {
	compacted := catalog.CompactedResult{
		CapabilityKey:   result.CapabilityKey,
		Source:          result.Source,
		Summary:         result.Summary,
		KeyFacts:        cloneStringMap(result.KeyFacts),
		FollowOnOptions: append([]string(nil), result.FollowOnOptions...),
		RawRef:          result.RawRef,
	}
	compacted.Bytes = catalog.CompactedSize(compacted)
	if err := broker.tracker.RecordCompaction(compacted.Bytes); err != nil {
		return catalog.CompactedResult{}, err
	}
	return compacted, nil
}

func (broker *Broker) Usage() budgets.Usage {
	return broker.tracker.Usage()
}

func cloneStringMap(values map[string]string) map[string]string {
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func normalizeSnapshot(snapshot registry.Snapshot) registry.Snapshot {
	if snapshot.ByKey == nil {
		snapshot.ByKey = make(map[string]registry.Item, len(snapshot.Items))
	}
	if snapshot.ByKind == nil {
		snapshot.ByKind = make(map[registry.Kind][]registry.Item)
	}

	for _, item := range snapshot.Items {
		if item.Key != "" {
			snapshot.ByKey[item.Key] = item
		}
		snapshot.ByKind[item.Kind] = append(snapshot.ByKind[item.Kind], item)
	}

	return snapshot
}

func (broker *Broker) currentSnapshot() (registry.Snapshot, error) {
	snapshot, err := broker.source.LoadSnapshot()
	if err != nil {
		return registry.Snapshot{}, err
	}
	return normalizeSnapshot(snapshot), nil
}

func structuredResultFromSkillInvocation(response skills.InvokeResponse) catalog.StructuredResult {
	return catalog.StructuredResult{
		CapabilityKey: response.SkillKey,
		Source:        "skill",
		Summary:       response.Summary,
		Artifacts:     append([]string(nil), response.Artifacts...),
		KeyFacts:      stringFacts(response.Output),
		RawRef:        response.RawRef,
		RawOutput:     response.RawOutput,
	}
}

func stringFacts(values map[string]any) map[string]string {
	if len(values) == 0 {
		return nil
	}

	facts := make(map[string]string)
	for key, value := range values {
		switch typed := value.(type) {
		case string:
			facts[key] = typed
		case fmt.Stringer:
			facts[key] = typed.String()
		case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, bool, float32, float64:
			facts[key] = fmt.Sprint(typed)
		}
	}
	if len(facts) == 0 {
		return nil
	}
	return facts
}
