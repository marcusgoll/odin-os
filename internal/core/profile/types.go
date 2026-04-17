package profile

import "time"

const DefaultWorkspaceID = "primary"

type OperatingProfile struct {
	WorkspaceID     string
	Preferences     Preferences
	Boundaries      Boundaries
	CadenceDefaults CadenceDefaults
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type Preferences struct {
	QuietHours string `json:"quiet_hours,omitempty"`
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
	RequireHumanApprovalForExternalEffects *bool
	ReviewCadence                          *string
}
