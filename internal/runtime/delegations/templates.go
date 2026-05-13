package delegations

import (
	"fmt"
	"strings"

	"odin-os/internal/registry"
)

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

func (service Service) childSpecsForAgent(agentKey string, inputs map[string]string) ([]ChildSpec, error) {
	agentKey = cleanInput(agentKey)
	if item, ok := service.RegistrySnapshot.ByKey[agentKey]; ok && item.Kind == registry.KindAgent {
		if !item.Delegation.Enabled {
			return nil, fmt.Errorf("agent %q is not runtime-delegatable", agentKey)
		}
		return childSpecsFromDelegationProfile(agentKey, item.Delegation, inputs)
	}
	return builtInChildSpecsForAgent(agentKey, inputs)
}

func builtInChildSpecsForAgent(agentKey string, inputs map[string]string) ([]ChildSpec, error) {
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

func childSpecsFromDelegationProfile(agentKey string, profile registry.DelegationProfile, inputs map[string]string) ([]ChildSpec, error) {
	if strings.TrimSpace(profile.OperatorSurface) != "companion_delegate" {
		return nil, fmt.Errorf("agent %q delegation surface %q is not supported", agentKey, profile.OperatorSurface)
	}
	for _, required := range profile.Inputs.Required {
		required = strings.TrimSpace(required)
		if required == "" {
			continue
		}
		if cleanInput(inputs[required]) == "" {
			return nil, fmt.Errorf("%s is required", required)
		}
	}
	if len(profile.Children) == 0 {
		return nil, fmt.Errorf("agent %q has no delegation children", agentKey)
	}

	childSpecs := make([]ChildSpec, 0, len(profile.Children))
	for _, child := range profile.Children {
		convergenceMode := cleanInput(child.ConvergenceMode)
		if convergenceMode == "" {
			convergenceMode = cleanInput(profile.ConvergenceMode)
		}
		if convergenceMode == "" {
			convergenceMode = "merge"
		}
		childSpecs = append(childSpecs, ChildSpec{
			DelegationKey:   cleanInput(child.DelegationKey),
			Role:            cleanInput(child.Role),
			Wave:            child.Wave,
			ActionClass:     cleanInput(child.ActionClass),
			ActionKey:       renderDelegationTemplate(child.ActionKeyTemplate, inputs),
			MutationMode:    delegationMutationModeFromProfile(child.MutationModeSource, inputs),
			ConvergenceMode: convergenceMode,
			ArtifactTarget:  cleanInput(child.ArtifactTarget),
			Executor:        cleanInput(child.Executor),
			SkillKey:        cleanInput(child.SkillKey),
		})
	}
	return childSpecs, nil
}

func renderDelegationTemplate(template string, inputs map[string]string) string {
	replacer := strings.NewReplacer(
		"{{portal_track}}", cleanInput(inputs["portal_track"]),
		"{{surface}}", cleanInput(inputs["surface"]),
		"{{goal}}", cleanInput(inputs["goal"]),
		"{{intent}}", delegationRunIntent(inputs["intent"]),
	)
	return cleanInput(replacer.Replace(template))
}

func delegationMutationModeFromProfile(source string, inputs map[string]string) string {
	switch strings.TrimSpace(source) {
	case "intent":
		return delegationRunIntent(inputs["intent"])
	default:
		return delegationRunIntent(source)
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
