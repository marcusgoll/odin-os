package commands

import "testing"

func TestParseApprovalSupportFilter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want ApprovalSupportFilter
	}{
		{name: "default all", args: nil, want: ApprovalSupportAll},
		{name: "explicit all", args: []string{"all"}, want: ApprovalSupportAll},
		{name: "supported", args: []string{"supported"}, want: ApprovalSupportSupported},
		{name: "unsupported", args: []string{"unsupported"}, want: ApprovalSupportUnsupported},
		{name: "trim and case fold", args: []string{" Supported "}, want: ApprovalSupportSupported},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := ParseApprovalSupportFilter(tt.args)
			if err != nil {
				t.Fatalf("ParseApprovalSupportFilter(%v) error = %v", tt.args, err)
			}
			if got != tt.want {
				t.Fatalf("ParseApprovalSupportFilter(%v) = %q, want %q", tt.args, got, tt.want)
			}
		})
	}
}

func TestParseApprovalSupportFilterRejectsUnknownInput(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{{"waiting"}, {"supported", "unsupported"}} {
		if _, err := ParseApprovalSupportFilter(args); err == nil {
			t.Fatalf("ParseApprovalSupportFilter(%v) error = nil, want usage error", args)
		}
	}
}

func TestApprovalSupportFilterMatchesResolverSupport(t *testing.T) {
	t.Parallel()

	if !ApprovalSupportAll.Matches("unsupported") || !ApprovalSupportAll.Matches("supported") {
		t.Fatal("ApprovalSupportAll should match every resolver support value")
	}
	if !ApprovalSupportSupported.Matches("supported") {
		t.Fatal("ApprovalSupportSupported should match supported resolver support")
	}
	if ApprovalSupportSupported.Matches("unsupported") {
		t.Fatal("ApprovalSupportSupported should not match unsupported resolver support")
	}
	if !ApprovalSupportUnsupported.Matches("unsupported") {
		t.Fatal("ApprovalSupportUnsupported should match unsupported resolver support")
	}
	if ApprovalSupportUnsupported.Matches("supported") {
		t.Fatal("ApprovalSupportUnsupported should not match supported resolver support")
	}
	if ApprovalSupportFilter("stale").Matches("supported") {
		t.Fatal("unknown filter should not match resolver support")
	}
}
