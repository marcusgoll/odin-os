package validator_test

import (
	"os"
	"path/filepath"
	"testing"

	"odin-os/internal/registry"
	"odin-os/internal/registry/parser"
	"odin-os/internal/registry/validator"
)

func TestValidateRequiresVersionedInvokableSchemas(t *testing.T) {
	documents := []registry.ParsedDocument{
		mustParseFixture(t, "skill-triage.md"),
		mustParseFixture(t, "command-project-status.md"),
		mustParseFixture(t, "workflow-project-status.md"),
	}

	diagnostics := validator.ValidateDocuments(documents)
	if len(diagnostics) != 0 {
		t.Fatalf("ValidateDocuments() diagnostics = %v, want none", diagnostics)
	}
}

func TestValidateDocumentsRejectsKindMismatch(t *testing.T) {
	document := registry.ParsedDocument{
		Source: registry.SourceFile{
			Path:         "/tmp/commands/triage.md",
			RelativePath: "commands/triage.md",
			ExpectedKind: registry.KindCommand,
		},
		Frontmatter: registry.Frontmatter{
			Kind:       registry.KindSkill,
			Key:        "triage",
			Title:      "Triage",
			Summary:    "Summary",
			Strictness: "rigid",
			AppliesTo:  []string{"intake"},
		},
		Sections: map[string]string{
			registry.SectionPurpose:         "Purpose",
			registry.SectionWhenToUse:       "When to use",
			registry.SectionInputs:          "Inputs",
			registry.SectionProcedure:       "Procedure",
			registry.SectionOutputs:         "Outputs",
			registry.SectionConstraints:     "Constraints",
			registry.SectionSuccessCriteria: "Success",
		},
	}

	diagnostics := validator.ValidateDocuments([]registry.ParsedDocument{document})
	if len(diagnostics) == 0 {
		t.Fatal("ValidateDocuments() diagnostics = 0, want at least 1")
	}

	if diagnostics[0].Code != "kind_mismatch" {
		t.Fatalf("diagnostic code = %q, want %q", diagnostics[0].Code, "kind_mismatch")
	}
}

func TestValidateDocumentsRejectsDuplicateKeys(t *testing.T) {
	documents := []registry.ParsedDocument{
		makeValidDocument("skills/triage-a.md", "triage"),
		makeValidDocument("skills/triage-b.md", "triage"),
	}

	diagnostics := validator.ValidateDocuments(documents)

	var duplicateCount int
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == "duplicate_key" {
			duplicateCount++
		}
	}

	if duplicateCount != 2 {
		t.Fatalf("duplicate diagnostics = %d, want 2", duplicateCount)
	}
}

func TestValidateDocumentsRejectsInvalidKindSpecificField(t *testing.T) {
	document := makeValidDocument("skills/triage.md", "triage")
	document.Frontmatter.Strictness = ""

	diagnostics := validator.ValidateDocuments([]registry.ParsedDocument{document})

	var found bool
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == "missing_field" && diagnostic.Message == "required frontmatter field strictness is missing" {
			found = true
		}
	}

	if !found {
		t.Fatal("expected missing strictness diagnostic")
	}
}

func mustParseFixture(t *testing.T, filename string) registry.ParsedDocument {
	t.Helper()

	content, err := os.ReadFile(filepath.Join("..", "testdata", "normalized", filename))
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", filename, err)
	}

	source := registry.SourceFile{
		Path:         filepath.Join("/tmp", filename),
		RelativePath: filepath.ToSlash(filepath.Join("testdata", "normalized", filename)),
		ExpectedKind: registry.KindSkill,
	}

	switch filename[0] {
	case 'c':
		source.ExpectedKind = registry.KindCommand
	case 'w':
		source.ExpectedKind = registry.KindWorkflow
	}

	document, diagnostics := parser.ParseSource(source, content)
	if len(diagnostics) != 0 {
		t.Fatalf("ParseSource(%q) diagnostics = %v, want none", filename, diagnostics)
	}

	return document
}

func makeValidDocument(relativePath string, key string) registry.ParsedDocument {
	return registry.ParsedDocument{
		Source: registry.SourceFile{
			Path:         "/tmp/" + relativePath,
			RelativePath: relativePath,
			ExpectedKind: registry.KindSkill,
		},
		Frontmatter: registry.Frontmatter{
			Kind:       registry.KindSkill,
			Key:        key,
			Title:      "Triage",
			Summary:    "Summary",
			Strictness: "rigid",
			AppliesTo:  []string{"intake"},
		},
		Sections: map[string]string{
			registry.SectionPurpose:         "Purpose",
			registry.SectionWhenToUse:       "When to use",
			registry.SectionInputs:          "Inputs",
			registry.SectionProcedure:       "Procedure",
			registry.SectionOutputs:         "Outputs",
			registry.SectionConstraints:     "Constraints",
			registry.SectionSuccessCriteria: "Success",
		},
	}
}
