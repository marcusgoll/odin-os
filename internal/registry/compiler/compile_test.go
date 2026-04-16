package compiler_test

import (
	"os"
	"path/filepath"
	"testing"

	"odin-os/internal/registry"
	"odin-os/internal/registry/compiler"
	"odin-os/internal/registry/parser"
)

func TestCompileProducesNormalizedDescriptors(t *testing.T) {
	documents := []registry.ParsedDocument{
		mustParseFixture(t, "skill-triage.md", registry.KindSkill),
		mustParseFixture(t, "command-project-status.md", registry.KindCommand),
		mustParseFixture(t, "workflow-project-status.md", registry.KindWorkflow),
	}

	snapshot := compiler.Compile(documents, nil)
	if len(snapshot.Diagnostics) != 0 {
		t.Fatalf("Compile() diagnostics = %v, want none", snapshot.Diagnostics)
	}

	if len(snapshot.Items) != 3 {
		t.Fatalf("Compile() items = %d, want 3", len(snapshot.Items))
	}

	item, ok := snapshot.ByKey["triage-skill"]
	if !ok {
		t.Fatal("snapshot.ByKey does not contain triage-skill")
	}

	if item.APIVersion != "odin/v1" {
		t.Fatalf("item.APIVersion = %q, want %q", item.APIVersion, "odin/v1")
	}

	if item.Name != "triage-skill" {
		t.Fatalf("item.Name = %q, want %q", item.Name, "triage-skill")
	}

	if item.Version == "" {
		t.Fatal("item.Version is empty, want versioned descriptor")
	}

	if item.Availability.Scope == "" {
		t.Fatal("item.Availability.Scope is empty")
	}

	if len(item.Permissions) == 0 {
		t.Fatal("item.Permissions is empty")
	}

	if item.InputSchema.Ref == "" {
		t.Fatal("item.InputSchema.Ref is empty")
	}

	if item.OutputSchema.Ref == "" {
		t.Fatal("item.OutputSchema.Ref is empty")
	}

	if len(item.Dependencies) == 0 {
		t.Fatal("item.Dependencies is empty")
	}

	if item.Execution.Mode == "" {
		t.Fatal("item.Execution.Mode is empty")
	}

	if item.Implementation.Kind == "" {
		t.Fatal("item.Implementation.Kind is empty")
	}

	workflow, ok := snapshot.ByKey["project-status-workflow"]
	if !ok {
		t.Fatal("snapshot.ByKey does not contain project-status-workflow")
	}
	if len(workflow.Scopes) != 1 || workflow.Scopes[0] != "project" {
		t.Fatalf("workflow.Scopes = %#v, want [project]", workflow.Scopes)
	}
}

func mustParseFixture(t *testing.T, filename string, kind registry.Kind) registry.ParsedDocument {
	t.Helper()

	content, err := os.ReadFile(filepath.Join("..", "testdata", "normalized", filename))
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", filename, err)
	}

	source := registry.SourceFile{
		Path:         filepath.Join("/tmp", filename),
		RelativePath: filepath.ToSlash(filepath.Join("testdata", "normalized", filename)),
		ExpectedKind: kind,
	}

	document, diagnostics := parser.ParseSource(source, content)
	if len(diagnostics) != 0 {
		t.Fatalf("ParseSource(%q) diagnostics = %v, want none", filename, diagnostics)
	}

	return document
}
