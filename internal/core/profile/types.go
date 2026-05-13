package profile

import (
	"time"

	coreworkspaces "odin-os/internal/core/workspaces"
)

const DefaultWorkspaceKey = coreworkspaces.DefaultWorkspaceKey

type OperatingProfile struct {
	WorkspaceID     int64
	WorkspaceKey    string
	Preferences     Preferences
	Boundaries      Boundaries
	CadenceDefaults CadenceDefaults
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type Preferences struct {
	QuietHours           string `json:"quiet_hours,omitempty"`
	NotificationsEnabled bool   `json:"notifications_enabled,omitempty"`
	NotificationBatching string `json:"notification_batching,omitempty"`
}

type Boundaries struct {
	ApprovalDefaults ApprovalDefaults `json:"approval_defaults"`
}

type ApprovalDefaults struct {
	RequireHumanApprovalForExternalEffects bool `json:"require_human_approval_for_external_effects"`
}

type CadenceDefaults struct {
	ReviewCadence string `json:"review_cadence,omitempty"`
}

type UpdateParams struct {
	QuietHours                             *string
	NotificationsEnabled                   *bool
	NotificationBatching                   *string
	RequireHumanApprovalForExternalEffects *bool
	ReviewCadence                          *string
}
