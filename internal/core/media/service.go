package media

import (
	"fmt"
	"strings"
)

type Service struct{}

func (Service) Validate(config Config) error {
	for index, service := range config.Services {
		if strings.TrimSpace(service.Name) == "" {
			return fmt.Errorf("media service %d name is required", index)
		}
		if strings.TrimSpace(string(service.Kind)) == "" {
			return fmt.Errorf("media service %q kind is required", service.Name)
		}
	}

	return validatePolicies(config.Policies)
}

func (Service) ClassifyAction(config Config, action string) AutomationClass {
	normalized := strings.TrimSpace(action)
	if normalized == "" {
		return AutomationClassApprovalRequired
	}

	if containsAction(config.Policies.Forbidden, normalized) {
		return AutomationClassForbidden
	}
	if containsAction(config.Policies.AutoAllowed, normalized) {
		return AutomationClassAutoAllowed
	}
	if containsAction(config.Policies.NotifyOnly, normalized) {
		return AutomationClassNotifyOnly
	}
	if containsAction(config.Policies.ApprovalRequired, normalized) {
		return AutomationClassApprovalRequired
	}

	return AutomationClassApprovalRequired
}

func validatePolicies(policies Policies) error {
	seen := map[string]AutomationClass{}
	for _, entry := range []struct {
		class   AutomationClass
		actions []string
	}{
		{class: AutomationClassAutoAllowed, actions: policies.AutoAllowed},
		{class: AutomationClassNotifyOnly, actions: policies.NotifyOnly},
		{class: AutomationClassApprovalRequired, actions: policies.ApprovalRequired},
		{class: AutomationClassForbidden, actions: policies.Forbidden},
	} {
		for _, action := range entry.actions {
			normalized := strings.TrimSpace(action)
			if normalized == "" {
				continue
			}
			if previous, exists := seen[normalized]; exists {
				return fmt.Errorf("media action %q is configured in both %q and %q", normalized, previous, entry.class)
			}
			seen[normalized] = entry.class
		}
	}

	return nil
}

func containsAction(actions []string, action string) bool {
	for _, candidate := range actions {
		if strings.TrimSpace(candidate) == action {
			return true
		}
	}
	return false
}
