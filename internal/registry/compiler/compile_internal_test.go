package compiler

import (
	"testing"

	"odin-os/internal/registry"
)

func TestCompileItemUsesCanonicalNormalizedName(t *testing.T) {
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
			Key:        "legacy-triage-key",
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
			registry.SectionWhenToUse:       "When to Use",
			registry.SectionInputs:          "Inputs",
			registry.SectionProcedure:       "Procedure",
			registry.SectionOutputs:         "Outputs",
			registry.SectionConstraints:     "Constraints",
			registry.SectionSuccessCriteria: "Success Criteria",
		},
	}

	item := compileItem(document)
	if item.Key != "triage-skill" {
		t.Fatalf("compileItem() key = %q, want canonical normalized name %q", item.Key, "triage-skill")
	}

	if item.Name != item.Key {
		t.Fatalf("compileItem() name = %q, key = %q, want canonical identity match", item.Name, item.Key)
	}
}
