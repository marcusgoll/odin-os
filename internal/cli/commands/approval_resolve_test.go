package commands

import "testing"

func TestParseApprovalResolve(t *testing.T) {
	t.Parallel()

	command, err := ParseApprovalResolve([]string{
		"resolve",
		"--id", "42",
		"--decision", "approve",
		"--reason", "safe to proceed",
		"--by", "operator",
		"--json",
	})
	if err != nil {
		t.Fatalf("ParseApprovalResolve() error = %v", err)
	}
	if command.Name != "resolve" {
		t.Fatalf("Name = %q, want resolve", command.Name)
	}
	if command.ApprovalID != 42 {
		t.Fatalf("ApprovalID = %d, want 42", command.ApprovalID)
	}
	if command.Decision != "approve" {
		t.Fatalf("Decision = %q, want approve", command.Decision)
	}
	if command.Reason != "safe to proceed" {
		t.Fatalf("Reason = %q, want safe to proceed", command.Reason)
	}
	if command.By != "operator" {
		t.Fatalf("By = %q, want operator", command.By)
	}
	if !command.JSON {
		t.Fatal("JSON = false, want true")
	}
}

func TestParseApprovalResolveRejectsMissingID(t *testing.T) {
	t.Parallel()

	if _, err := ParseApprovalResolve([]string{
		"resolve",
		"--decision", "approve",
		"--reason", "safe to proceed",
		"--by", "operator",
	}); err == nil {
		t.Fatal("ParseApprovalResolve() error = nil, want missing id error")
	}
}
