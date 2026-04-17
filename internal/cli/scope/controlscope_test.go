package scope

import (
	"testing"

	"odin-os/internal/core/controlscope"
)

func TestControlScopeFromResolutionTranslatesProjectTargets(t *testing.T) {
	t.Parallel()

	got := ToControlScope(Resolution{
		Kind:       ScopeProject,
		ProjectKey: "alpha",
	})
	want := controlscope.ControlScope{
		SubjectType: controlscope.SubjectTypeProject,
		SubjectKey:  "alpha",
		ProjectKey:  "alpha",
	}

	if got != want {
		t.Fatalf("ToControlScope() = %+v, want %+v", got, want)
	}
}
