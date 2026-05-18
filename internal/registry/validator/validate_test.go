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

func TestValidateNormalizedManifestRejectsWorkflowDependencyKind(t *testing.T) {
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
					Kind:    registry.KindWorkflow,
					Name:    "project-status-workflow",
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

	if diagnostics[0].Code != "invalid_dependency" {
		t.Fatalf("diagnostic code = %q, want %q", diagnostics[0].Code, "invalid_dependency")
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
			Kind:           registry.KindSkill,
			Key:            "triage",
			Title:          "Triage",
			Summary:        "Summary",
			Version:        "1.0.0",
			Enabled:        boolPtr(true),
			Strictness:     "rigid",
			AppliesTo:      []string{"intake"},
			Scopes:         []string{"project"},
			Permissions:    []string{"repo.read"},
			HandlerType:    "command",
			HandlerRef:     "scripts/skills/triage.sh",
			TimeoutSeconds: 15,
			LegacyInputSchema: map[string]any{
				"type": "object",
			},
			LegacyOutputSchema: map[string]any{
				"type": "object",
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
	if len(diagnostics) == 0 {
		t.Fatal("ValidateDocuments() diagnostics = 0, want at least 1")
	}

	assertDiagnosticCode(t, diagnostics, "kind_mismatch")
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

func TestValidateDocumentsRejectsInvalidRegistryKey(t *testing.T) {
	document := makeValidDocument("skills/triage.md", "../triage")

	diagnostics := validator.ValidateDocuments([]registry.ParsedDocument{document})

	assertDiagnosticCode(t, diagnostics, "invalid_key")
}

func TestValidateDocumentsRejectsWhitespacePaddedKey(t *testing.T) {
	document := makeValidDocument("skills/triage.md", " triage ")

	diagnostics := validator.ValidateDocuments([]registry.ParsedDocument{document})

	assertDiagnosticCode(t, diagnostics, "invalid_key")
	assertDiagnosticMessage(t, diagnostics, `registry key " triage " must not include leading or trailing whitespace`)
}

func TestValidateDocumentsRejectsMissingExecutableSkillFields(t *testing.T) {
	document := makeValidDocument("skills/triage.md", "triage")
	document.Frontmatter.Version = ""
	document.Frontmatter.Enabled = nil
	document.Frontmatter.HandlerType = ""
	document.Frontmatter.HandlerRef = ""
	document.Frontmatter.TimeoutSeconds = 0
	document.Frontmatter.Permissions = nil
	document.Frontmatter.Scopes = nil
	document.Frontmatter.LegacyInputSchema = nil
	document.Frontmatter.LegacyOutputSchema = nil

	diagnostics := validator.ValidateDocuments([]registry.ParsedDocument{document})

	assertDiagnosticMessage(t, diagnostics, "required frontmatter field version is missing")
	assertDiagnosticMessage(t, diagnostics, "required frontmatter field enabled is missing")
	assertDiagnosticMessage(t, diagnostics, "required frontmatter field scopes is missing")
	assertDiagnosticMessage(t, diagnostics, "required frontmatter field permissions is missing")
	assertDiagnosticMessage(t, diagnostics, "required frontmatter field handler_type is missing")
	assertDiagnosticMessage(t, diagnostics, "required frontmatter field handler_ref is missing")
	assertDiagnosticMessage(t, diagnostics, "required frontmatter field timeout_seconds is missing")
	assertDiagnosticMessage(t, diagnostics, "required frontmatter field input_schema is missing")
	assertDiagnosticMessage(t, diagnostics, "required frontmatter field output_schema is missing")
}

func TestValidateDocumentsRejectsUnsupportedSkillHandlerType(t *testing.T) {
	document := makeValidDocument("skills/triage.md", "triage")
	document.Frontmatter.HandlerType = "builtin"

	diagnostics := validator.ValidateDocuments([]registry.ParsedDocument{document})

	assertDiagnosticCode(t, diagnostics, "invalid_handler_type")
}

func TestValidateDocumentsRejectsAbsoluteSkillHandlerRef(t *testing.T) {
	document := makeValidDocument("skills/triage.md", "triage")
	document.Frontmatter.HandlerRef = "/tmp/triage.sh"

	diagnostics := validator.ValidateDocuments([]registry.ParsedDocument{document})

	assertDiagnosticCode(t, diagnostics, "invalid_handler_ref")
}

func TestValidateDocumentsRejectsMissingSkillHandlerTarget(t *testing.T) {
	document := makeValidDocument("skills/triage.md", "triage")

	diagnostics := validator.ValidateDocuments([]registry.ParsedDocument{document})

	assertDiagnosticCode(t, diagnostics, "invalid_handler_ref")
	assertDiagnosticMessage(t, diagnostics, `skill handler_ref "scripts/skills/triage.sh" must point to an existing executable file`)
}

func TestValidateDocumentsRejectsEscapingSkillHandlerRef(t *testing.T) {
	document := makeValidDocument("skills/triage.md", "triage")
	document.Frontmatter.HandlerRef = "../outside.sh"

	diagnostics := validator.ValidateDocuments([]registry.ParsedDocument{document})

	assertDiagnosticCode(t, diagnostics, "invalid_handler_ref")
}

func TestValidateDocumentsRejectsHandlerOutsideSkillScriptsRoot(t *testing.T) {
	document := makeValidDocument("skills/triage.md", "triage")
	document.Frontmatter.HandlerRef = "scripts/other/triage.sh"

	diagnostics := validator.ValidateDocuments([]registry.ParsedDocument{document})

	assertDiagnosticCode(t, diagnostics, "invalid_handler_ref")
	assertDiagnosticMessage(t, diagnostics, `skill handler_ref "scripts/other/triage.sh" must stay under scripts/skills`)
}

func TestValidateDocumentsRejectsSymlinkedHandlerOutsideAllowedRoot(t *testing.T) {
	repoRoot := t.TempDir()
	targetPath := filepath.Join(repoRoot, "tools", "outside.sh")
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(target dir) error = %v", err)
	}
	if err := os.WriteFile(targetPath, []byte("#!/usr/bin/env bash\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(target) error = %v", err)
	}

	handlerPath := filepath.Join(repoRoot, "scripts", "skills", "linked.sh")
	if err := os.MkdirAll(filepath.Dir(handlerPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(handler dir) error = %v", err)
	}
	if err := os.Symlink(targetPath, handlerPath); err != nil {
		t.Fatalf("Symlink(handler) error = %v", err)
	}

	document := makeValidDocument("skills/triage.md", "triage")
	document.Source.Path = filepath.Join(repoRoot, "registry", "skills", "triage.md")
	document.Frontmatter.HandlerRef = "scripts/skills/linked.sh"

	diagnostics := validator.ValidateDocuments([]registry.ParsedDocument{document})

	assertDiagnosticCode(t, diagnostics, "invalid_handler_ref")
	assertDiagnosticMessage(t, diagnostics, `skill handler_ref "scripts/skills/linked.sh" must resolve under scripts/skills`)
}

func TestValidateDocumentsRejectsHandlerThroughSymlinkedDirectoryOutsideAllowedRoot(t *testing.T) {
	repoRoot := t.TempDir()
	outsideDir := filepath.Join(repoRoot, "tools")
	if err := os.MkdirAll(outsideDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(outside dir) error = %v", err)
	}
	targetPath := filepath.Join(outsideDir, "handler.sh")
	if err := os.WriteFile(targetPath, []byte("#!/usr/bin/env bash\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(target) error = %v", err)
	}

	handlerDir := filepath.Join(repoRoot, "scripts", "skills")
	if err := os.MkdirAll(handlerDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(handler dir) error = %v", err)
	}
	symlinkDir := filepath.Join(handlerDir, "linkdir")
	if err := os.Symlink(outsideDir, symlinkDir); err != nil {
		t.Fatalf("Symlink(linkdir) error = %v", err)
	}

	document := makeValidDocument("skills/triage.md", "triage")
	document.Source.Path = filepath.Join(repoRoot, "registry", "skills", "triage.md")
	document.Frontmatter.HandlerRef = "scripts/skills/linkdir/handler.sh"

	diagnostics := validator.ValidateDocuments([]registry.ParsedDocument{document})

	assertDiagnosticCode(t, diagnostics, "invalid_handler_ref")
	assertDiagnosticMessage(t, diagnostics, `skill handler_ref "scripts/skills/linkdir/handler.sh" must resolve under scripts/skills`)
}

func TestValidateDocumentsAcceptsHandlerWhenRepoRootIsSymlink(t *testing.T) {
	realRepoRoot := t.TempDir()
	handlerPath := filepath.Join(realRepoRoot, "scripts", "skills", "triage.sh")
	if err := os.MkdirAll(filepath.Dir(handlerPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(handler dir) error = %v", err)
	}
	if err := os.WriteFile(handlerPath, []byte("#!/usr/bin/env bash\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(handler) error = %v", err)
	}

	linkParent := t.TempDir()
	linkRepoRoot := filepath.Join(linkParent, "odin-os-live")
	if err := os.Symlink(realRepoRoot, linkRepoRoot); err != nil {
		t.Fatalf("Symlink(repo root) error = %v", err)
	}

	document := makeValidDocument("skills/triage.md", "triage")
	document.Source.Path = filepath.Join(linkRepoRoot, "registry", "skills", "triage.md")
	document.Frontmatter.HandlerRef = "scripts/skills/triage.sh"

	diagnostics := validator.ValidateDocuments([]registry.ParsedDocument{document})
	if len(diagnostics) != 0 {
		t.Fatalf("ValidateDocuments() diagnostics = %+v, want none", diagnostics)
	}
}

func TestValidateDocumentsRejectsExistingDirectoryHandlerTarget(t *testing.T) {
	repoRoot := t.TempDir()
	handlerDir := filepath.Join(repoRoot, "scripts", "skills", "handler-dir")
	if err := os.MkdirAll(handlerDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(handler dir) error = %v", err)
	}

	document := makeValidDocument("skills/triage.md", "triage")
	document.Source.Path = filepath.Join(repoRoot, "registry", "skills", "triage.md")
	document.Frontmatter.HandlerRef = "scripts/skills/handler-dir"

	diagnostics := validator.ValidateDocuments([]registry.ParsedDocument{document})

	assertDiagnosticCode(t, diagnostics, "invalid_handler_ref")
	assertDiagnosticMessage(t, diagnostics, `skill handler_ref "scripts/skills/handler-dir" must point to an existing executable file`)
}

func TestValidateDocumentsRejectsExistingNonExecutableHandlerTarget(t *testing.T) {
	repoRoot := t.TempDir()
	handlerPath := filepath.Join(repoRoot, "scripts", "skills", "plain.sh")
	if err := os.MkdirAll(filepath.Dir(handlerPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(handler dir) error = %v", err)
	}
	if err := os.WriteFile(handlerPath, []byte("#!/usr/bin/env bash\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(handler) error = %v", err)
	}

	document := makeValidDocument("skills/triage.md", "triage")
	document.Source.Path = filepath.Join(repoRoot, "registry", "skills", "triage.md")
	document.Frontmatter.HandlerRef = "scripts/skills/plain.sh"

	diagnostics := validator.ValidateDocuments([]registry.ParsedDocument{document})

	assertDiagnosticCode(t, diagnostics, "invalid_handler_ref")
	assertDiagnosticMessage(t, diagnostics, `skill handler_ref "scripts/skills/plain.sh" must point to an existing executable file`)
}

func TestValidateDocumentsRejectsInvalidSkillTimeout(t *testing.T) {
	document := makeValidDocument("skills/triage.md", "triage")
	document.Frontmatter.TimeoutSeconds = 1000

	diagnostics := validator.ValidateDocuments([]registry.ParsedDocument{document})

	assertDiagnosticCode(t, diagnostics, "invalid_timeout")
}

func TestValidateDocumentsRejectsNonObjectSkillSchema(t *testing.T) {
	document := makeValidDocument("skills/triage.md", "triage")
	document.Frontmatter.LegacyInputSchema = map[string]any{
		"type": "string",
	}

	diagnostics := validator.ValidateDocuments([]registry.ParsedDocument{document})

	assertDiagnosticCode(t, diagnostics, "invalid_schema")
}

func TestValidateDocumentsRejectsUnknownPermission(t *testing.T) {
	document := makeValidDocument("skills/triage.md", "triage")
	document.Frontmatter.Permissions = []string{"repo.write"}

	diagnostics := validator.ValidateDocuments([]registry.ParsedDocument{document})

	assertDiagnosticCode(t, diagnostics, "invalid_permission")
	assertDiagnosticMessage(t, diagnostics, `invalid permission "repo.write"`)
}

func TestValidateDocumentsRejectsMalformedIsolatedPermission(t *testing.T) {
	document := makeValidDocument("skills/triage.md", "triage")
	document.Frontmatter.Permissions = []string{"repo.mutate.isolated:"}

	diagnostics := validator.ValidateDocuments([]registry.ParsedDocument{document})

	assertDiagnosticCode(t, diagnostics, "invalid_permission")
	assertDiagnosticMessage(t, diagnostics, `invalid permission "repo.mutate.isolated:"`)
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
			Kind:           registry.KindSkill,
			Key:            key,
			Title:          "Triage",
			Summary:        "Summary",
			Version:        "1.0.0",
			Enabled:        boolPtr(true),
			Strictness:     "rigid",
			AppliesTo:      []string{"intake"},
			Scopes:         []string{"project"},
			Permissions:    []string{"repo.read"},
			HandlerType:    "command",
			HandlerRef:     "scripts/skills/triage.sh",
			TimeoutSeconds: 15,
			LegacyInputSchema: map[string]any{
				"type": "object",
			},
			LegacyOutputSchema: map[string]any{
				"type": "object",
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
}

func assertDiagnosticCode(t *testing.T, diagnostics []registry.Diagnostic, want string) {
	t.Helper()

	for _, diagnostic := range diagnostics {
		if diagnostic.Code == want {
			return
		}
	}

	t.Fatalf("diagnostics missing code %q: %+v", want, diagnostics)
}

func assertDiagnosticMessage(t *testing.T, diagnostics []registry.Diagnostic, want string) {
	t.Helper()

	for _, diagnostic := range diagnostics {
		if diagnostic.Message == want {
			return
		}
	}

	t.Fatalf("diagnostics missing message %q: %+v", want, diagnostics)
}

func boolPtr(value bool) *bool {
	return &value
}
