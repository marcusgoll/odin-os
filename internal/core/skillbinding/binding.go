package skillbinding

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	ArtifactType = "skill_invocation"
	WorkKind     = "skill_invocation"
)

type Binding struct {
	SkillKey              string          `json:"skill_key"`
	SkillVersion          string          `json:"skill_version,omitempty"`
	InputJSON             json.RawMessage `json:"input_json"`
	SourceType            string          `json:"source_type"`
	SourceID              string          `json:"source_id,omitempty"`
	SourceKey             string          `json:"source_key,omitempty"`
	Scope                 string          `json:"scope"`
	ProjectKey            string          `json:"project_key,omitempty"`
	ExecutionIntent       string          `json:"execution_intent"`
	ExecutionIntentSource string          `json:"execution_intent_source"`
	ReviewState           string          `json:"review_state"`
}

type Artifact struct {
	Type            string  `json:"type"`
	SkillInvocation Binding `json:"skill_invocation"`
}

func Normalize(binding Binding) (Binding, error) {
	binding.SkillKey = strings.TrimSpace(binding.SkillKey)
	binding.SkillVersion = strings.TrimSpace(binding.SkillVersion)
	binding.SourceType = strings.TrimSpace(binding.SourceType)
	binding.SourceID = strings.TrimSpace(binding.SourceID)
	binding.SourceKey = strings.TrimSpace(binding.SourceKey)
	binding.Scope = strings.TrimSpace(binding.Scope)
	binding.ProjectKey = strings.TrimSpace(binding.ProjectKey)
	binding.ExecutionIntent = strings.TrimSpace(binding.ExecutionIntent)
	binding.ExecutionIntentSource = strings.TrimSpace(binding.ExecutionIntentSource)
	binding.ReviewState = strings.TrimSpace(binding.ReviewState)
	if binding.SkillKey == "" {
		return Binding{}, fmt.Errorf("skill invocation skill_key is required")
	}
	if binding.InputJSON == nil || strings.TrimSpace(string(binding.InputJSON)) == "" {
		binding.InputJSON = json.RawMessage(`{}`)
	}
	if !json.Valid(binding.InputJSON) {
		return Binding{}, fmt.Errorf("skill invocation input_json must be valid JSON")
	}
	if binding.Scope == "" {
		binding.Scope = "project"
	}
	if binding.ExecutionIntent == "" {
		binding.ExecutionIntent = "read_only"
	}
	switch binding.ExecutionIntent {
	case "read_only", "mutation", "governance", "destructive":
	default:
		return Binding{}, fmt.Errorf("skill invocation execution_intent must be one of read_only, mutation, governance, destructive")
	}
	if binding.ReviewState == "" {
		binding.ReviewState = "review_required"
	}
	return binding, nil
}

func EncodeArtifacts(binding Binding) (string, error) {
	normalized, err := Normalize(binding)
	if err != nil {
		return "", err
	}
	encoded, err := json.Marshal([]Artifact{{
		Type:            ArtifactType,
		SkillInvocation: normalized,
	}})
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func DecodeArtifacts(raw string) (Binding, bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "[]" {
		return Binding{}, false, nil
	}
	var artifacts []Artifact
	if err := json.Unmarshal([]byte(raw), &artifacts); err != nil {
		return Binding{}, false, err
	}
	for _, artifact := range artifacts {
		if strings.TrimSpace(artifact.Type) != ArtifactType {
			continue
		}
		binding, err := Normalize(artifact.SkillInvocation)
		return binding, true, err
	}
	return Binding{}, false, nil
}

func InputMap(binding Binding) (map[string]any, error) {
	normalized, err := Normalize(binding)
	if err != nil {
		return nil, err
	}
	var input map[string]any
	if err := json.Unmarshal(normalized.InputJSON, &input); err != nil {
		return nil, err
	}
	return input, nil
}
