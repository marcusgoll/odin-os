package commands

import "testing"

func TestParseSlashCommand(t *testing.T) {
	t.Parallel()

	command, ok := Parse("/project alpha")
	if !ok {
		t.Fatalf("Parse() ok = false, want true")
	}
	if command.Name != "project" {
		t.Fatalf("Name = %q, want project", command.Name)
	}
	if len(command.Args) != 1 || command.Args[0] != "alpha" {
		t.Fatalf("Args = %#v, want [alpha]", command.Args)
	}
}

func TestParseRejectsNonSlashInput(t *testing.T) {
	t.Parallel()

	if _, ok := Parse("what is my scope?"); ok {
		t.Fatalf("Parse() ok = true, want false")
	}
}

func TestRouteAskIntent(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		input string
		want  Intent
	}{
		{input: "what scope am i in?", want: IntentScope},
		{input: "show approvals waiting", want: IntentApprovals},
		{input: "show runs", want: IntentRuns},
		{input: "show logs", want: IntentLogs},
		{input: "help", want: IntentHelp},
		{input: "can you run through the release plan?", want: IntentUnknown},
		{input: "log this idea for later", want: IntentUnknown},
		{input: "write a refactor", want: IntentUnknown},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.input, func(t *testing.T) {
			t.Parallel()

			if got := RouteAskIntent(testCase.input); got != testCase.want {
				t.Fatalf("RouteAskIntent(%q) = %q, want %q", testCase.input, got, testCase.want)
			}
		})
	}
}
