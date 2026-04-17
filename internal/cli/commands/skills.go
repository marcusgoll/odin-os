package commands

import (
	"encoding/json"
	"os"

	"odin-os/internal/skills"
)

type SkillsView struct {
	Skills []skills.Skill `json:"skills"`
}

type SkillDeleteView struct {
	Key     string `json:"key"`
	Deleted bool   `json:"deleted"`
}

func LoadSkillSpecFile(path string) (skills.SkillSpec, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return skills.SkillSpec{}, err
	}

	var spec skills.SkillSpec
	if err := json.Unmarshal(content, &spec); err != nil {
		return skills.SkillSpec{}, err
	}
	return spec, nil
}

func DecodeSkillInput(raw string) (map[string]any, error) {
	if raw == "" {
		return nil, nil
	}

	var input map[string]any
	if err := json.Unmarshal([]byte(raw), &input); err != nil {
		return nil, err
	}
	return input, nil
}
