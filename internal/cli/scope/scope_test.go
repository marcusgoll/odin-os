package scope

import (
	"testing"

	corescope "odin-os/internal/core/scope"
)

func TestResolutionResolveScope(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		input ResolveInput
		want  Kind
	}{
		{
			name:  "global by default",
			input: ResolveInput{},
			want:  ScopeGlobal,
		},
		{
			name: "new project flow",
			input: ResolveInput{
				NewProjectFlow: true,
			},
			want: ScopeNewProject,
		},
		{
			name: "system project explicit target",
			input: ResolveInput{
				ExplicitTarget: &Target{
					ProjectKey:    "odin-core",
					SystemProject: true,
				},
			},
			want: ScopeOdinCore,
		},
		{
			name: "managed project explicit target",
			input: ResolveInput{
				ExplicitTarget: &Target{
					ProjectKey: "alpha",
				},
			},
			want: ScopeProject,
		},
		{
			name: "explicit target beats hint",
			input: ResolveInput{
				ExplicitTarget: &Target{
					ProjectKey:    "odin-core",
					SystemProject: true,
				},
				CWDHintProjectKey: "alpha",
			},
			want: ScopeOdinCore,
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := Resolve(testCase.input)
			if got.Kind != testCase.want {
				t.Fatalf("scope kind = %q, want %q", got.Kind, testCase.want)
			}
		})
	}
}

func TestResolutionControlScope(t *testing.T) {
	t.Parallel()

	got := Resolve(ResolveInput{
		ExplicitTarget: &Target{
			ProjectKey:    "alpha",
			SystemProject: false,
		},
	}).ControlScope()

	want := corescope.ControlScope{
		SubjectType:   corescope.SubjectTypeInitiative,
		SubjectKey:    "alpha",
		WorkspaceKey:  "default",
		InitiativeKey: "alpha",
		ProjectKey:    "alpha",
		CompanionKey:  "primary",
	}

	if got != want {
		t.Fatalf("ControlScope() = %+v, want %+v", got, want)
	}
}
