package skills

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateSkillWritesCanonicalRegistryFile(t *testing.T) {
	t.Parallel()

	service := newTestService(t)

	skill, err := service.Create(context.Background(), minimalSkillSpec("echo-skill"))
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if skill.Key != "echo-skill" {
		t.Fatalf("skill.Key = %q, want %q", skill.Key, "echo-skill")
	}

	path := filepath.Join(service.RepoRoot, "registry", "skills", "echo-skill.md")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	if !strings.Contains(string(content), "handler_type: command") {
		t.Fatalf("canonical file missing handler type:\n%s", string(content))
	}
}

func TestListAndGetReadSkillsFromCanonicalRegistry(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	if _, err := service.Create(context.Background(), minimalSkillSpec("echo-skill")); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	skills, err := service.List(context.Background())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(skills) != 1 {
		t.Fatalf("List() len = %d, want 1", len(skills))
	}

	skill, err := service.Get(context.Background(), "echo-skill")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if skill.Title != "Echo Skill" {
		t.Fatalf("skill.Title = %q, want %q", skill.Title, "Echo Skill")
	}
}

func TestCreateRejectsDuplicateSkillKey(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	if _, err := service.Create(context.Background(), minimalSkillSpec("echo-skill")); err != nil {
		t.Fatalf("first Create() error = %v", err)
	}

	if _, err := service.Create(context.Background(), minimalSkillSpec("echo-skill")); err == nil {
		t.Fatal("second Create() error = nil, want duplicate rejection")
	}
}

func TestCreateRejectsMissingHandlerTarget(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	spec := minimalSkillSpec("missing-handler")
	spec.HandlerRef = "scripts/skills/missing-handler.sh"

	if _, err := service.Create(context.Background(), spec); err == nil {
		t.Fatal("Create() error = nil, want missing handler rejection")
	}
}

func TestCreateRejectsNonExecutableHandler(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	handlerPath := filepath.Join(service.RepoRoot, "scripts", "skills", "plain-handler.sh")
	writeFile(t, handlerPath, "#!/usr/bin/env bash\nprintf '%s\\n' '{}'\n")

	spec := minimalSkillSpec("plain-handler")
	spec.HandlerRef = "scripts/skills/plain-handler.sh"

	_, err := service.Create(context.Background(), spec)
	if err == nil || !strings.Contains(err.Error(), "must point to an existing executable file") {
		t.Fatalf("Create() error = %v, want executable-file rejection", err)
	}
}

func TestCreateRejectsEscapingSkillKey(t *testing.T) {
	t.Parallel()

	service := newTestService(t)

	_, err := service.Create(context.Background(), minimalSkillSpec("../../outside-skill"))
	if err == nil || !strings.Contains(err.Error(), "registry key") {
		t.Fatalf("Create() error = %v, want invalid key rejection", err)
	}

	escapedPath := filepath.Join(service.RepoRoot, "outside-skill.md")
	if _, statErr := os.Stat(escapedPath); !os.IsNotExist(statErr) {
		t.Fatalf("Stat(%q) error = %v, want not exists", escapedPath, statErr)
	}
}

func TestCreateRejectsWhitespacePaddedSkillKey(t *testing.T) {
	t.Parallel()

	service := newTestService(t)

	_, err := service.Create(context.Background(), minimalSkillSpec(" echo-skill "))
	if err == nil || !strings.Contains(err.Error(), "must not include leading or trailing whitespace") {
		t.Fatalf("Create() error = %v, want whitespace key rejection", err)
	}
}

func TestUpdateSkillRewritesCanonicalFile(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	if _, err := service.Create(context.Background(), minimalSkillSpec("echo-skill")); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	spec := minimalSkillSpec("echo-skill")
	spec.Summary = "Updated summary."
	spec.Version = "1.0.1"

	skill, err := service.Update(context.Background(), "echo-skill", spec)
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if skill.Summary != "Updated summary." {
		t.Fatalf("skill.Summary = %q, want updated summary", skill.Summary)
	}

	content := mustReadFile(t, filepath.Join(service.RepoRoot, "registry", "skills", "echo-skill.md"))
	if !strings.Contains(content, "summary: Updated summary.") {
		t.Fatalf("canonical file not updated:\n%s", content)
	}
}

func TestUpdateSkillIsAtomicOnValidationFailure(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	if _, err := service.Create(context.Background(), minimalSkillSpec("echo-skill")); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	path := filepath.Join(service.RepoRoot, "registry", "skills", "echo-skill.md")
	original := mustReadFile(t, path)

	spec := minimalSkillSpec("echo-skill")
	spec.HandlerRef = ""

	if _, err := service.Update(context.Background(), "echo-skill", spec); err == nil {
		t.Fatal("Update() error = nil, want validation failure")
	}

	current := mustReadFile(t, path)
	if current != original {
		t.Fatalf("canonical file changed after failed update\nwant:\n%s\n\ngot:\n%s", original, current)
	}
}

func TestDeleteSkillRemovesCanonicalFile(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	if _, err := service.Create(context.Background(), minimalSkillSpec("echo-skill")); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if err := service.Delete(context.Background(), "echo-skill"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	path := filepath.Join(service.RepoRoot, "registry", "skills", "echo-skill.md")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("Stat(%q) error = %v, want not exists", path, err)
	}
}

func newTestService(t *testing.T) Service {
	t.Helper()

	repoRoot := t.TempDir()
	for _, dir := range []string{
		filepath.Join(repoRoot, "registry", "agents"),
		filepath.Join(repoRoot, "registry", "skills"),
		filepath.Join(repoRoot, "registry", "workflows"),
		filepath.Join(repoRoot, "registry", "commands"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll(%q) error = %v", dir, err)
		}
	}
	writeExecutable(t, filepath.Join(repoRoot, "scripts", "skills", "echo-skill.sh"), `#!/usr/bin/env bash
cat >/dev/null
printf '%s\n' '{"status":"ok","summary":"default"}'
`)

	return Service{RepoRoot: repoRoot}
}

func minimalSkillSpec(key string) SkillSpec {
	return SkillSpec{
		Key:            key,
		Title:          "Echo Skill",
		Summary:        "Echoes a structured response.",
		Status:         "active",
		Version:        "1.0.0",
		Enabled:        true,
		Tags:           []string{"testing"},
		Owners:         []string{"odin-core"},
		Strictness:     "rigid",
		AppliesTo:      []string{"testing"},
		Scopes:         []string{"project"},
		Permissions:    []string{"repo.read"},
		HandlerType:    "command",
		HandlerRef:     "scripts/skills/echo-skill.sh",
		TimeoutSeconds: 15,
		InputSchema: map[string]any{
			"type": "object",
		},
		OutputSchema: map[string]any{
			"type": "object",
		},
		Sections: map[string]string{
			"Purpose":          "Echo back a normalized response.",
			"When to Use":      "When testing skill execution.",
			"Inputs":           "A message string.",
			"Procedure":        "Read the input and echo a stable response.",
			"Outputs":          "A JSON summary.",
			"Constraints":      "Remain deterministic.",
			"Success Criteria": "The caller receives a stable response.",
		},
	}
}

func mustReadFile(t *testing.T, path string) string {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	return string(content)
}
