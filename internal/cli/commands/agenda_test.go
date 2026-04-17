package commands

import "testing"

func TestAgendaParseJSON(t *testing.T) {
	t.Parallel()

	command, err := ParseAgenda([]string{"--json"})
	if err != nil {
		t.Fatalf("ParseAgenda() error = %v", err)
	}
	if !command.JSON {
		t.Fatal("JSON = false, want true")
	}
}

func TestAgendaParseRejectsUnexpectedArgs(t *testing.T) {
	t.Parallel()

	if _, err := ParseAgenda([]string{"extra"}); err == nil {
		t.Fatal("ParseAgenda() error = nil, want usage error")
	}
}
