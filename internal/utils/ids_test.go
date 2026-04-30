package utils

import "testing"

func TestNormalizeIDCurrentlyTrimsAndLowercasesOnly(t *testing.T) {
	t.Parallel()

	got := NormalizeID("  Odin Core/Issue 42  ")
	want := "odin core/issue 42"
	if got != want {
		t.Fatalf("NormalizeID() = %q, want %q", got, want)
	}
}
