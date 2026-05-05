package delegations

import "fmt"

type ChildSpec struct {
	DelegationKey   string
	Role            string
	Wave            int
	ActionClass     string
	ActionKey       string
	MutationMode    string
	ConvergenceMode string
	ArtifactTarget  string
	Executor        string
	SkillKey        string
}

func childSpecsForAgent(agentKey string, inputs map[string]string) ([]ChildSpec, error) {
	switch agentKey {
	case "portal-delivery-agent":
		portalTrack := cleanInput(inputs["portal_track"])
		surface := cleanInput(inputs["surface"])
		if portalTrack == "" {
			return nil, fmt.Errorf("portal_track is required")
		}
		if surface == "" {
			return nil, fmt.Errorf("surface is required")
		}
		actionKey := fmt.Sprintf("%s:%s", portalTrack, surface)
		mutationMode := delegationRunIntent(inputs["intent"])
		return []ChildSpec{
			{
				DelegationKey:   "ia-audit",
				Role:            "ia_audit",
				Wave:            delegationRoleWave("ia_audit"),
				ActionClass:     "portal_delivery",
				ActionKey:       actionKey,
				MutationMode:    mutationMode,
				ConvergenceMode: "parent_summary",
				ArtifactTarget:  "run_detail",
				Executor:        "codex_headless",
			},
			{
				DelegationKey:   "design-direction",
				Role:            "design_direction",
				Wave:            delegationRoleWave("design_direction"),
				ActionClass:     "portal_delivery",
				ActionKey:       actionKey,
				MutationMode:    mutationMode,
				ConvergenceMode: "parent_summary",
				ArtifactTarget:  "run_detail",
				Executor:        "codex_headless",
				SkillKey:        "pixel-perfect-ui-ux-designer",
			},
			{
				DelegationKey:   "implementation-handoff",
				Role:            "implementation_handoff",
				Wave:            delegationRoleWave("implementation_handoff"),
				ActionClass:     "portal_delivery",
				ActionKey:       actionKey,
				MutationMode:    mutationMode,
				ConvergenceMode: "parent_summary",
				ArtifactTarget:  "run_detail",
				Executor:        "codex_headless",
			},
			{
				DelegationKey:   "visual-verification",
				Role:            "visual_verification",
				Wave:            delegationRoleWave("visual_verification"),
				ActionClass:     "portal_delivery",
				ActionKey:       actionKey,
				MutationMode:    mutationMode,
				ConvergenceMode: "parent_summary",
				ArtifactTarget:  "run_detail",
				Executor:        "codex_headless",
				SkillKey:        "pixel-perfect-ui-ux-designer",
			},
			{
				DelegationKey:   "learning-capture",
				Role:            "learning_capture",
				Wave:            delegationRoleWave("learning_capture"),
				ActionClass:     "portal_delivery",
				ActionKey:       actionKey,
				MutationMode:    mutationMode,
				ConvergenceMode: "parent_summary",
				ArtifactTarget:  "run_detail",
				Executor:        "codex_headless",
			},
		}, nil
	default:
		return nil, fmt.Errorf("unsupported agent %q", agentKey)
	}
}

func delegationRoleWave(role string) int {
	switch role {
	case "ia_audit", "design_direction":
		return 1
	case "implementation_handoff", "visual_verification":
		return 2
	case "learning_capture":
		return 3
	default:
		return 99
	}
}
