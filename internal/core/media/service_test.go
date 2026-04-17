package media

import "testing"

func TestMediaClassifyActionReturnsConfiguredClasses(t *testing.T) {
	t.Parallel()

	config := Config{
		Policies: Policies{
			AutoAllowed:      []string{"media_probe_cycle"},
			NotifyOnly:       []string{"media_maintenance_candidate"},
			ApprovalRequired: []string{"restart_plex"},
			Forbidden:        []string{"delete_media"},
		},
	}

	service := Service{}

	if got := service.ClassifyAction(config, "media_probe_cycle"); got != AutomationClassAutoAllowed {
		t.Fatalf("ClassifyAction(auto) = %q, want %q", got, AutomationClassAutoAllowed)
	}
	if got := service.ClassifyAction(config, "media_maintenance_candidate"); got != AutomationClassNotifyOnly {
		t.Fatalf("ClassifyAction(notify) = %q, want %q", got, AutomationClassNotifyOnly)
	}
	if got := service.ClassifyAction(config, "restart_plex"); got != AutomationClassApprovalRequired {
		t.Fatalf("ClassifyAction(approval) = %q, want %q", got, AutomationClassApprovalRequired)
	}
	if got := service.ClassifyAction(config, "delete_media"); got != AutomationClassForbidden {
		t.Fatalf("ClassifyAction(forbidden) = %q, want %q", got, AutomationClassForbidden)
	}
}

func TestMediaClassifyActionDefaultsUnknownToApprovalRequired(t *testing.T) {
	t.Parallel()

	service := Service{}

	if got := service.ClassifyAction(Config{}, "unknown_action"); got != AutomationClassApprovalRequired {
		t.Fatalf("ClassifyAction(unknown) = %q, want %q", got, AutomationClassApprovalRequired)
	}
}

func TestMediaValidateRejectsServiceWithoutRequiredIdentifiers(t *testing.T) {
	t.Parallel()

	err := Service{}.Validate(Config{
		Services: []StackService{
			{
				Name: "",
				Kind: ServiceKindPlex,
			},
		},
	})
	if err == nil {
		t.Fatalf("Validate() error = nil, want validation failure")
	}
}
