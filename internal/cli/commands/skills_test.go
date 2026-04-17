package commands

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSkillSpecFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "skill.json")
	if err := os.WriteFile(path, []byte(`{
  "key": "echo-skill",
  "title": "Echo Skill",
  "summary": "Echoes a stable response.",
  "status": "active",
  "version": "1.0.0",
  "enabled": true,
  "tags": ["testing"],
  "owners": ["odin-core"],
  "strictness": "rigid",
  "applies_to": ["testing"],
  "scopes": ["project"],
  "permissions": ["repo.read"],
  "handler_type": "command",
  "handler_ref": "scripts/skills/echo-skill.sh",
  "timeout_seconds": 15,
  "input_schema": {"type":"object"},
  "output_schema": {"type":"object"},
  "sections": {
    "Purpose": "Echo input.",
    "When to Use": "When testing.",
    "Inputs": "A message.",
    "Procedure": "Read and echo.",
    "Outputs": "A JSON response.",
    "Constraints": "Stay deterministic.",
    "Success Criteria": "The caller gets a stable response."
  }
}`), 0o644); err != nil {
		t.Fatalf("WriteFile(spec) error = %v", err)
	}

	spec, err := LoadSkillSpecFile(path)
	if err != nil {
		t.Fatalf("LoadSkillSpecFile() error = %v", err)
	}
	if spec.Key != "echo-skill" {
		t.Fatalf("spec.Key = %q, want echo-skill", spec.Key)
	}
	if spec.HandlerType != "command" {
		t.Fatalf("spec.HandlerType = %q, want command", spec.HandlerType)
	}
}

func TestDecodeSkillInput(t *testing.T) {
	t.Parallel()

	input, err := DecodeSkillInput(`{"message":"hello"}`)
	if err != nil {
		t.Fatalf("DecodeSkillInput() error = %v", err)
	}
	if input["message"] != "hello" {
		t.Fatalf("input[message] = %#v, want hello", input["message"])
	}
}
