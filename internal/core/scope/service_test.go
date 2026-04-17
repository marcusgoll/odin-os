package scope

import "testing"

func TestControlScopeResolvesLegacyScope(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input LegacyScope
		want  ControlScope
	}{
		{
			name:  "global",
			input: LegacyScope{Kind: "global"},
			want: ControlScope{
				SubjectType:  SubjectTypeWorkspace,
				SubjectKey:   "default",
				WorkspaceKey: "default",
				CompanionKey: "primary",
			},
		},
		{
			name:  "project",
			input: LegacyScope{Kind: "project", ProjectKey: "alpha"},
			want: ControlScope{
				SubjectType:   SubjectTypeInitiative,
				SubjectKey:    "alpha",
				WorkspaceKey:  "default",
				InitiativeKey: "alpha",
				ProjectKey:    "alpha",
				CompanionKey:  "primary",
			},
		},
		{
			name:  "new project flow",
			input: LegacyScope{Kind: "new-project"},
			want: ControlScope{
				SubjectType:   SubjectTypeNewProject,
				SubjectKey:    "odin-core",
				WorkspaceKey:  "default",
				InitiativeKey: "odin-core",
				ProjectKey:    "odin-core",
				CompanionKey:  "primary",
			},
		},
	}

	for _, testCase := range tests {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := ResolveLegacy(testCase.input)
			if got != testCase.want {
				t.Fatalf("ResolveLegacy() = %+v, want %+v", got, testCase.want)
			}
		})
	}
}
