package branches

import "testing"

func TestNameBuildsTaskOwnedBranch(t *testing.T) {
	t.Parallel()

	name := Name(NameParams{
		ProjectKey: "cfipros",
		TaskID:     42,
		RunID:      9,
		Try:        1,
	})

	want := "odin/cfipros/task-42/run-9/try-1"
	if name != want {
		t.Fatalf("Name() = %q, want %q", name, want)
	}
}

func TestNameSanitizesProjectKeyAndOmitsAgentIdentity(t *testing.T) {
	t.Parallel()

	name := Name(NameParams{
		ProjectKey: "odin core/repo",
		TaskID:     7,
		RunID:      3,
		Try:        2,
	})

	want := "odin/odin-core-repo/task-7/run-3/try-2"
	if name != want {
		t.Fatalf("Name() = %q, want %q", name, want)
	}
	if contains(name, "agent") {
		t.Fatalf("Name() = %q, want no agent identity", name)
	}
}

func TestNextTryIncrementsRetrySuffix(t *testing.T) {
	t.Parallel()

	next := NextTry("odin/cfipros/task-42/run-9/try-1")
	want := "odin/cfipros/task-42/run-9/try-2"
	if next != want {
		t.Fatalf("NextTry() = %q, want %q", next, want)
	}
}

func contains(value string, needle string) bool {
	return len(needle) > 0 && len(value) > 0 && stringIndex(value, needle) >= 0
}

func stringIndex(value string, needle string) int {
	for idx := range value {
		if len(value[idx:]) < len(needle) {
			return -1
		}
		if value[idx:idx+len(needle)] == needle {
			return idx
		}
	}
	return -1
}
