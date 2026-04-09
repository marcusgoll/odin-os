package broker

import (
	"fmt"

	"odin-os/internal/registry"
	"odin-os/internal/tools/budgets"
	"odin-os/internal/tools/catalog"
)

type Broker struct {
	snapshot registry.Snapshot
	builtins map[string]catalog.ToolDefinition
	tracker  *budgets.Tracker
}

func New(snapshot registry.Snapshot, builtins map[string]catalog.ToolDefinition, limits budgets.Limits) *Broker {
	snapshot = normalizeSnapshot(snapshot)
	return &Broker{
		snapshot: snapshot,
		builtins: builtins,
		tracker:  budgets.NewTracker(limits),
	}
}

func (broker *Broker) Catalog(scope string) []catalog.Card {
	scope = catalog.NormalizeScope(scope)
	cards := make([]catalog.Card, 0, len(broker.builtins)+len(broker.snapshot.Items))

	for _, definition := range broker.builtins {
		if catalog.MatchesScope(definition.Scopes, scope) {
			cards = append(cards, definition.Card())
		}
	}

	for _, item := range broker.snapshot.Items {
		card, ok := catalog.CardFromRegistry(item)
		if !ok {
			continue
		}
		if catalog.MatchesScope(card.Scopes, scope) {
			cards = append(cards, card)
		}
	}

	catalog.SortCards(cards)
	return cards
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

	item, ok := broker.snapshot.ByKey[key]
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
				Key:       item.Key,
				Title:     item.Title,
				Summary:   item.Summary,
				Tags:      append([]string(nil), item.Tags...),
				Scopes:    append([]string(nil), item.Scopes...),
				Sections:  catalog.CloneSections(item.Sections),
				SourceRef: item.Source.RelativePath,
			},
		}, nil
	case registry.KindAgent:
		return catalog.Expansion{
			Card: card,
			SubAgent: &catalog.SubAgentDefinition{
				Key:       item.Key,
				Title:     item.Title,
				Summary:   item.Summary,
				Tags:      append([]string(nil), item.Tags...),
				Scopes:    append([]string(nil), item.Scopes...),
				Tools:     append([]string(nil), item.Tools...),
				Role:      item.Role,
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
	return definition.Invoke(input)
}

func (broker *Broker) Compact(result catalog.StructuredResult) (catalog.CompactedResult, error) {
	compacted := catalog.CompactedResult{
		CapabilityKey:   result.CapabilityKey,
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
