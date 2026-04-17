package approvals

import (
	"testing"

	coremedia "odin-os/internal/core/media"
)

func TestMediaApprovalDecisionRequiresApprovalForRestartAction(t *testing.T) {
	t.Parallel()

	decision := Service{}.Evaluate(coremedia.Config{
		Policies: coremedia.Policies{
			ApprovalRequired: []string{"restart_plex", "retry_import_move"},
			Forbidden:        []string{"delete_media", "change_vpn_networking"},
		},
	}, "restart_plex")

	if decision.Class != coremedia.AutomationClassApprovalRequired {
		t.Fatalf("Class = %q, want %q", decision.Class, coremedia.AutomationClassApprovalRequired)
	}
	if !decision.RequiresApproval {
		t.Fatalf("RequiresApproval = false, want true")
	}
	if !decision.Allowed {
		t.Fatalf("Allowed = false, want true")
	}
}

func TestMediaApprovalDecisionRejectsForbiddenAction(t *testing.T) {
	t.Parallel()

	decision := Service{}.Evaluate(coremedia.Config{
		Policies: coremedia.Policies{
			Forbidden: []string{"delete_media", "change_vpn_networking"},
		},
	}, "delete_media")

	if decision.Class != coremedia.AutomationClassForbidden {
		t.Fatalf("Class = %q, want %q", decision.Class, coremedia.AutomationClassForbidden)
	}
	if decision.Allowed {
		t.Fatalf("Allowed = true, want false")
	}
}
