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
		if !knownServiceKinds[service.Kind] {
			return fmt.Errorf("media service %q kind %q is unknown", service.Name, service.Kind)
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
			if !knownActions[normalized] {
				return fmt.Errorf("media action %q is unknown", normalized)
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

var knownServiceKinds = map[ServiceKind]bool{
	ServiceKindPlex:       true,
	ServiceKindRadarr:     true,
	ServiceKindSonarr:     true,
	ServiceKindProwlarr:   true,
	ServiceKindDownloader: true,
	ServiceKindVPN:        true,
	ServiceKindSeedbox:    true,
	ServiceKindUsenet:     true,
	ServiceKindSync:       true,
}

var knownActions = map[string]bool{
	"media_probe_cycle":           true,
	"media_mount_audit":           true,
	"media_backup_gate":           true,
	"media_queue_audit":           true,
	"media_import_audit":          true,
	"media_maintenance_candidate": true,
	"restart_plex":                true,
	"restart_arr":                 true,
	"restart_downloader":          true,
	"retry_import_move":           true,
	"queue_mutation":              true,
	"delete_media":                true,
	"delete_downloader_payload":   true,
	"remap_root_folders":          true,
	"rotate_media_credentials":    true,
	"change_vpn_networking":       true,
}
