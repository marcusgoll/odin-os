package memory

import (
	"encoding/json"
	"fmt"
	"strings"
)

type EntryType string

const (
	EntryTypeNote       EntryType = "note"
	EntryTypeSummary    EntryType = "summary"
	EntryTypeTranscript EntryType = "transcript"
	EntryTypeEpisode    EntryType = "episode"
)

type VisibilityScope string

const (
	VisibilityWorkspace  VisibilityScope = "workspace"
	VisibilityInitiative VisibilityScope = "initiative"
	VisibilityCompanion  VisibilityScope = "companion"
	VisibilityRun        VisibilityScope = "run"
)

type RetentionClass string

const (
	RetentionDurable  RetentionClass = "durable"
	RetentionWorking  RetentionClass = "working"
	RetentionEpisodic RetentionClass = "episodic"
)

type WriteInput struct {
	EntryType       EntryType
	VisibilityScope VisibilityScope
	RetentionClass  RetentionClass
	Summary         string
	Content         string
	MetadataJSON    string
}

const (
	MemoryTypeOperatingProfileUpdate = "operating_profile_update"
	MemoryTypeFollowUpCompletion     = "follow_up_completion"
	MemoryTypeFollowUpOverdue        = "follow_up_overdue"
)

func NormalizeWriteInput(input WriteInput) (WriteInput, error) {
	input.EntryType = EntryType(strings.ToLower(strings.TrimSpace(string(input.EntryType))))
	input.VisibilityScope = VisibilityScope(strings.ToLower(strings.TrimSpace(string(input.VisibilityScope))))
	input.RetentionClass = RetentionClass(strings.ToLower(strings.TrimSpace(string(input.RetentionClass))))
	input.Summary = strings.TrimSpace(input.Summary)
	input.Content = strings.TrimSpace(input.Content)
	input.MetadataJSON = strings.TrimSpace(input.MetadataJSON)

	if input.EntryType == "" {
		return WriteInput{}, fmt.Errorf("memory entry type is required")
	}
	if input.VisibilityScope == "" {
		return WriteInput{}, fmt.Errorf("memory visibility scope is required")
	}
	if input.RetentionClass == "" {
		return WriteInput{}, fmt.Errorf("memory retention class is required")
	}
	if input.Content == "" {
		return WriteInput{}, fmt.Errorf("memory content is required")
	}
	if input.MetadataJSON == "" {
		input.MetadataJSON = `{}`
	}
	if !json.Valid([]byte(input.MetadataJSON)) {
		return WriteInput{}, fmt.Errorf("invalid memory metadata JSON")
	}

	return input, nil
}
