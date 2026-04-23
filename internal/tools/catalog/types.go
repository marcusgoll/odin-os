package catalog

import (
	"sort"
	"strings"

	"odin-os/internal/registry"
)

type Kind string

const (
	KindTool     Kind = "tool"
	KindSkill    Kind = "skill"
	KindSubAgent Kind = "sub_agent"
)

type CostHint string

const (
	CostHintLow    CostHint = "low"
	CostHintMedium CostHint = "medium"
	CostHintHigh   CostHint = "high"
)

type Card struct {
	Kind         Kind
	Key          string
	CanonicalKey string
	Title        string
	Summary      string
	Hidden       bool
	Scopes       []string
	Tags         []string
	CostHint     CostHint
	BudgetCost   int
	SourceRef    string
}

type ToolDefinition struct {
	Key          string
	CanonicalKey string
	Aliases      []string
	Title        string
	Summary      string
	Hidden       bool
	Scopes       []string
	Tags         []string
	CostHint     CostHint
	BudgetCost   int
	SourceRef    string
	Schema       map[string]any
	Invoke       func(map[string]string) (StructuredResult, error)
}

type SkillDefinition struct {
	Key       string
	Title     string
	Summary   string
	Tags      []string
	Scopes    []string
	Sections  map[string]string
	SourceRef string
}

type SubAgentDefinition struct {
	Key       string
	Title     string
	Summary   string
	Tags      []string
	Scopes    []string
	Tools     []string
	Role      string
	Sections  map[string]string
	SourceRef string
}

type Expansion struct {
	Card     Card
	Tool     *ToolDefinition
	Skill    *SkillDefinition
	SubAgent *SubAgentDefinition
}

type StructuredResult struct {
	CapabilityKey   string
	Summary         string
	Artifacts       []string
	KeyFacts        map[string]string
	FollowOnOptions []string
	MemoryRecords   []MemoryRecord
	RawRef          string
	RawOutput       string
}

type MemoryRecord struct {
	MemoryType string
	Summary    string
	Fields     map[string]string
}

type CompactedResult struct {
	CapabilityKey   string
	Summary         string
	KeyFacts        map[string]string
	FollowOnOptions []string
	RawRef          string
	Bytes           int
}

func (definition ToolDefinition) Card() Card {
	canonicalKey := definition.CanonicalKey
	if canonicalKey == "" {
		canonicalKey = definition.Key
	}

	return Card{
		Kind:         KindTool,
		Key:          definition.Key,
		CanonicalKey: canonicalKey,
		Title:        definition.Title,
		Summary:      definition.Summary,
		Hidden:       definition.Hidden,
		Scopes:       append([]string(nil), definition.Scopes...),
		Tags:         append([]string(nil), definition.Tags...),
		CostHint:     definition.CostHint,
		BudgetCost:   definition.BudgetCost,
		SourceRef:    definition.SourceRef,
	}
}

func CardFromRegistry(item registry.Item) (Card, bool) {
	switch item.Kind {
	case registry.KindSkill:
		return Card{
			Kind:       KindSkill,
			Key:        item.Key,
			Title:      item.Title,
			Summary:    item.Summary,
			Scopes:     append([]string(nil), item.Scopes...),
			Tags:       append([]string(nil), item.Tags...),
			CostHint:   CostHintLow,
			BudgetCost: 1,
			SourceRef:  item.Source.RelativePath,
		}, true
	case registry.KindAgent:
		return Card{
			Kind:       KindSubAgent,
			Key:        item.Key,
			Title:      item.Title,
			Summary:    item.Summary,
			Scopes:     append([]string(nil), item.Scopes...),
			Tags:       append([]string(nil), item.Tags...),
			CostHint:   CostHintMedium,
			BudgetCost: 2,
			SourceRef:  item.Source.RelativePath,
		}, true
	default:
		return Card{}, false
	}
}

func SortCards(cards []Card) {
	sort.Slice(cards, func(i int, j int) bool {
		if cards[i].Kind != cards[j].Kind {
			return cards[i].Kind < cards[j].Kind
		}
		return cards[i].Key < cards[j].Key
	})
}

func MatchesScope(scopes []string, requested string) bool {
	if len(scopes) == 0 || requested == "" {
		return true
	}
	for _, scope := range scopes {
		if scope == requested {
			return true
		}
		if requested == "project" && scope == "managed-project" {
			return true
		}
	}
	return false
}

func CloneSections(sections map[string]string) map[string]string {
	cloned := make(map[string]string, len(sections))
	for key, value := range sections {
		cloned[key] = value
	}
	return cloned
}

func CompactedSize(result CompactedResult) int {
	size := len(result.CapabilityKey) + len(result.Summary) + len(result.RawRef)
	for key, value := range result.KeyFacts {
		size += len(key) + len(value)
	}
	for _, option := range result.FollowOnOptions {
		size += len(option)
	}
	return size
}

func NormalizeScope(scope string) string {
	return strings.TrimSpace(strings.ToLower(scope))
}
