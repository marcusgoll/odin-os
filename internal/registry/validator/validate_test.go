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

func TestValidateNormalizedAgentDoesNotRequireSchemas(t *testing.T) {
	document := registry.ParsedDocument{
		Source: registry.SourceFile{
			Path:         "/tmp/agents/triage-agent.md",
			RelativePath: "agents/triage-agent.md",
			ExpectedKind: registry.KindAgent,
		},
		Frontmatter: registry.Frontmatter{
			APIVersion: registry.NormalizedAPIVersion,
			Kind:       registry.KindAgent,
			Name:       "triage-agent",
			Version:    "1.0.0",
			Availability: registry.Availability{
				Scope: "global",
			},
			Permissions: []string{"filesystem", "web"},
			Dependencies: []registry.DependencyRef{
				{
					Kind:    registry.KindSkill,
					Name:    "triage-skill",
					Version: "1.0.0",
				},
			},
			Execution: registry.ExecutionPolicy{
				Mode: "local",
			},
			Implementation: registry.ImplementationRef{
				Kind: "markdown",
				Path: "agents/triage-agent.md",
			},
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
	if len(diagnostics) != 0 {
		t.Fatalf("ValidateDocuments() diagnostics = %v, want none", diagnostics)
	}
}

func TestValidateNormalizedManifestRejectsDivergingKey(t *testing.T) {
	document := registry.ParsedDocument{
		Source: registry.SourceFile{
			Path:         "/tmp/skills/triage.md",
			RelativePath: "skills/triage.md",
			ExpectedKind: registry.KindSkill,
		},
		Frontmatter: registry.Frontmatter{
			APIVersion: registry.NormalizedAPIVersion,
			Kind:       registry.KindSkill,
			Name:       "triage-skill",
			Key:        "triage-skill-legacy",
			Version:    "1.0.0",
			Availability: registry.Availability{
				Scope: "global",
			},
			Permissions: []string{"filesystem"},
			InputSchema: registry.SchemaRef{
				Ref: "schema://odin/skills/triage-skill/input",
			},
			OutputSchema: registry.SchemaRef{
				Ref: "schema://odin/skills/triage-skill/output",
			},
			Dependencies: []registry.DependencyRef{
				{
					Kind:    registry.KindAgent,
					Name:    "triage-agent",
					Version: "1.0.0",
				},
			},
			Execution: registry.ExecutionPolicy{
				Mode: "local",
			},
			Implementation: registry.ImplementationRef{
				Kind: "markdown",
				Path: "skills/triage.md",
			},
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
	if len(diagnostics) != 1 {
		t.Fatalf("ValidateDocuments() diagnostics = %v, want 1 diagnostic", diagnostics)
	}

	if diagnostics[0].Code != "invalid_identity" {
		t.Fatalf("diagnostic code = %q, want %q", diagnostics[0].Code, "invalid_identity")
	}
}

func TestValidateNormalizedInvokableRejectsMissingSchemas(t *testing.T) {
	document := registry.ParsedDocument{
		Source: registry.SourceFile{
			Path:         "/tmp/commands/project-status.md",
			RelativePath: "commands/project-status.md",
			ExpectedKind: registry.KindCommand,
		},
		Frontmatter: registry.Frontmatter{
			APIVersion: registry.NormalizedAPIVersion,
			Kind:       registry.KindCommand,
			Name:       "project-status",
			Version:    "1.0.0",
			Availability: registry.Availability{
				Scope: "global",
			},
			Permissions: []string{"filesystem"},
			Dependencies: []registry.DependencyRef{
				{
					Kind:    registry.KindSkill,
					Name:    "triage-skill",
					Version: "1.0.0",
				},
			},
			Execution: registry.ExecutionPolicy{
				Mode: "local",
			},
			Implementation: registry.ImplementationRef{
				Kind: "markdown",
				Path: "commands/project-status.md",
			},
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
	if len(diagnostics) != 2 {
		t.Fatalf("ValidateDocuments() diagnostics = %v, want 2 diagnostics", diagnostics)
	}

	foundInput := false
	foundOutput := false
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == "missing_field" && diagnostic.Message == "required frontmatter field inputSchema is missing" {
			foundInput = true
		}
		if diagnostic.Code == "missing_field" && diagnostic.Message == "required frontmatter field outputSchema is missing" {
			foundOutput = true
		}
	}

	if !foundInput || !foundOutput {
		t.Fatalf("ValidateDocuments() diagnostics = %v, want missing inputSchema and outputSchema", diagnostics)
	}
}

func TestValidateNormalizedManifestRejectsIncompleteDependency(t *testing.T) {
	document := registry.ParsedDocument{
		Source: registry.SourceFile{
			Path:         "/tmp/skills/triage.md",
			RelativePath: "skills/triage.md",
			ExpectedKind: registry.KindSkill,
		},
		Frontmatter: registry.Frontmatter{
			APIVersion: registry.NormalizedAPIVersion,
			Kind:       registry.KindSkill,
			Name:       "triage-skill",
			Version:    "1.0.0",
			Availability: registry.Availability{
				Scope: "global",
			},
			Permissions: []string{"filesystem"},
			InputSchema: registry.SchemaRef{
				Ref: "schema://odin/skills/triage-skill/input",
			},
			OutputSchema: registry.SchemaRef{
				Ref: "schema://odin/skills/triage-skill/output",
			},
			Dependencies: []registry.DependencyRef{
				{
					Name: "triage-agent",
				},
			},
			Execution: registry.ExecutionPolicy{
				Mode: "local",
			},
			Implementation: registry.ImplementationRef{
				Kind: "markdown",
				Path: "skills/triage.md",
			},
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
	if len(diagnostics) != 1 {
		t.Fatalf("ValidateDocuments() diagnostics = %v, want 1 diagnostic", diagnostics)
	}

	if diagnostics[0].Code != "invalid_dependency" {
		t.Fatalf("diagnostic code = %q, want %q", diagnostics[0].Code, "invalid_dependency")
	}
}

func TestValidateNormalizedManifestAcceptsToolDependencyKind(t *testing.T) {
	document := registry.ParsedDocument{
		Source: registry.SourceFile{
			Path:         "/tmp/skills/triage.md",
			RelativePath: "skills/triage.md",
			ExpectedKind: registry.KindSkill,
		},
		Frontmatter: registry.Frontmatter{
			APIVersion: registry.NormalizedAPIVersion,
			Kind:       registry.KindSkill,
			Name:       "triage-skill",
			Version:    "1.0.0",
			Availability: registry.Availability{
				Scope: "global",
			},
			Permissions: []string{"filesystem"},
			InputSchema: registry.SchemaRef{
				Ref: "schema://odin/skills/triage-skill/input",
			},
			OutputSchema: registry.SchemaRef{
				Ref: "schema://odin/skills/triage-skill/output",
			},
			Dependencies: []registry.DependencyRef{
				{
					Kind:    registry.Kind("tool"),
					Name:    "triage-tool",
					Version: "1.0.0",
				},
			},
			Execution: registry.ExecutionPolicy{
				Mode: "local",
			},
			Implementation: registry.ImplementationRef{
				Kind: "markdown",
				Path: "skills/triage.md",
			},
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
	if len(diagnostics) != 0 {
		t.Fatalf("ValidateDocuments() diagnostics = %v, want none", diagnostics)
	}
}

func TestValidateNormalizedManifestRejectsMissingImplementationPath(t *testing.T) {
	document := registry.ParsedDocument{
		Source: registry.SourceFile{
			Path:         "/tmp/skills/triage.md",
			RelativePath: "skills/triage.md",
			ExpectedKind: registry.KindSkill,
		},
		Frontmatter: registry.Frontmatter{
			APIVersion: registry.NormalizedAPIVersion,
			Kind:       registry.KindSkill,
			Name:       "triage-skill",
			Version:    "1.0.0",
			Availability: registry.Availability{
				Scope: "global",
			},
			Permissions: []string{"filesystem"},
			InputSchema: registry.SchemaRef{
				Ref: "schema://odin/skills/triage-skill/input",
			},
			OutputSchema: registry.SchemaRef{
				Ref: "schema://odin/skills/triage-skill/output",
			},
			Dependencies: []registry.DependencyRef{
				{
					Kind:    registry.KindAgent,
					Name:    "triage-agent",
					Version: "1.0.0",
				},
			},
			Execution: registry.ExecutionPolicy{
				Mode: "local",
			},
			Implementation: registry.ImplementationRef{
				Kind: "markdown",
			},
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
	if len(diagnostics) != 1 {
		t.Fatalf("ValidateDocuments() diagnostics = %v, want 1 diagnostic", diagnostics)
	}

	if diagnostics[0].Code != "missing_field" || diagnostics[0].Message != "required frontmatter field implementation.path is missing" {
		t.Fatalf("diagnostic = %+v, want missing implementation.path", diagnostics[0])
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

func TestValidateDocumentsRejectsUnsupportedAPIVersion(t *testing.T) {
	document := registry.ParsedDocument{
		Source: registry.SourceFile{
			Path:         "/tmp/skills/triage.md",
			RelativePath: "skills/triage.md",
			ExpectedKind: registry.KindSkill,
		},
		Frontmatter: registry.Frontmatter{
			APIVersion: registry.NormalizedAPIVersion + "-beta",
			Kind:       registry.KindSkill,
			Name:       "triage-skill",
			Version:    "1.0.0",
			Availability: registry.Availability{
				Scope: "global",
			},
			Permissions: []string{"filesystem"},
			InputSchema: registry.SchemaRef{
				Ref: "schema://odin/skills/triage-skill/input",
			},
			OutputSchema: registry.SchemaRef{
				Ref: "schema://odin/skills/triage-skill/output",
			},
			Dependencies: []registry.DependencyRef{
				{
					Kind:    registry.KindAgent,
					Name:    "triage-agent",
					Version: "1.0.0",
				},
			},
			Execution: registry.ExecutionPolicy{
				Mode: "local",
			},
			Implementation: registry.ImplementationRef{
				Kind: "markdown",
				Path: "skills/triage.md",
			},
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
	if len(diagnostics) != 1 {
		t.Fatalf("ValidateDocuments() diagnostics = %v, want 1 diagnostic", diagnostics)
	}

	if diagnostics[0].Code != "unsupported_api_version" {
		t.Fatalf("diagnostic code = %q, want %q", diagnostics[0].Code, "unsupported_api_version")
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
