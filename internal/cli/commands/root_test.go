package commands

import "testing"

func TestParseRootDefaultsToHelp(t *testing.T) {
	t.Parallel()

	cmd := ParseRoot(nil)
	if cmd.Name != "help" {
		t.Fatalf("Name = %q, want help", cmd.Name)
	}
}

func TestParseRootRoutesExplicitRepl(t *testing.T) {
	t.Parallel()

	cmd := ParseRoot([]string{"repl"})
	if cmd.Name != "repl" {
		t.Fatalf("Name = %q, want repl", cmd.Name)
	}
}
