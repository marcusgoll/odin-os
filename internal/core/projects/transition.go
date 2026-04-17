package projects

import "fmt"

type TransitionState string

const (
	TransitionStateInventory      TransitionState = "inventory"
	TransitionStateShadow         TransitionState = "shadow"
	TransitionStateCompare        TransitionState = "compare"
	TransitionStateLimitedAction  TransitionState = "limited_action"
	TransitionStateCutover        TransitionState = "cutover"
	TransitionStateDecommissioned TransitionState = "decommissioned"
)

type TransitionController string

const (
	TransitionControllerLegacyOdin TransitionController = "legacy_odin"
	TransitionControllerOdinOS     TransitionController = "odin_os"
)

type ActionClass string

const (
	ActionClassReadOnly            ActionClass = "read_only"
	ActionClassIsolatedMutation    ActionClass = "isolated_mutation"
	ActionClassFullMutation        ActionClass = "full_mutation"
	ActionClassGovernanceMutation  ActionClass = "governance_mutation"
	ActionClassDestructiveMutation ActionClass = "destructive_mutation"
	ActionClassTransitionControl   ActionClass = "transition_control"
)

type RuntimeTransition struct {
	State          TransitionState
	Controller     TransitionController
	LimitedActions []string
}

type TransitionAuthRequest struct {
	Transition  RuntimeTransition
	Actor       TransitionController
	ActionClass ActionClass
	ActionKey   string
}

type TransitionChangeRequest struct {
	Actor       TransitionController
	TargetState TransitionState
}

type TransitionDecision struct {
	Allowed bool
	Reason  string
}

func AuthorizeTransitionAction(request TransitionAuthRequest) TransitionDecision {
	if request.ActionClass == ActionClassReadOnly {
		return TransitionDecision{Allowed: true}
	}

	if request.Actor != request.Transition.Controller {
		return TransitionDecision{
			Allowed: false,
			Reason:  fmt.Sprintf("controller %q does not own mutation authority", request.Actor),
		}
	}

	switch request.Transition.State {
	case TransitionStateInventory, TransitionStateShadow, TransitionStateCompare:
		return TransitionDecision{
			Allowed: false,
			Reason:  fmt.Sprintf("transition state %q is read-only", request.Transition.State),
		}
	case TransitionStateLimitedAction:
		if request.ActionClass != ActionClassIsolatedMutation {
			return TransitionDecision{
				Allowed: false,
				Reason:  "limited_action only allows isolated mutation",
			}
		}
		if !containsString(request.Transition.LimitedActions, request.ActionKey) {
			return TransitionDecision{
				Allowed: false,
				Reason:  fmt.Sprintf("limited action %q is not allowlisted", request.ActionKey),
			}
		}
		return TransitionDecision{Allowed: true}
	case TransitionStateCutover, TransitionStateDecommissioned:
		return TransitionDecision{Allowed: true}
	default:
		return TransitionDecision{
			Allowed: false,
			Reason:  fmt.Sprintf("unsupported transition state %q", request.Transition.State),
		}
	}
}

func ValidateTransitionChange(current RuntimeTransition, request TransitionChangeRequest) TransitionDecision {
	if request.TargetState == "" {
		return TransitionDecision{
			Allowed: false,
			Reason:  "target transition state is required",
		}
	}
	if request.Actor != TransitionControllerOdinOS {
		return TransitionDecision{
			Allowed: false,
			Reason:  fmt.Sprintf("controller %q cannot change transition state", request.Actor),
		}
	}
	if current.State == request.TargetState {
		return TransitionDecision{
			Allowed: false,
			Reason:  "target transition state must differ from current state",
		}
	}
	return TransitionDecision{Allowed: true}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

type MutationAssignment struct {
	BranchName   string
	WorktreePath string
}

func ValidateSystemProjectMutation(manifest Manifest) error {
	if manifest.SystemProject && manifest.Policy.ApprovalGates.RequireForSystemProjectChanges != nil && *manifest.Policy.ApprovalGates.RequireForSystemProjectChanges {
		return fmt.Errorf("system project %q requires explicit approval for mutations", manifest.Key)
	}
	return nil
}

func ValidateMutationAssignment(manifest Manifest, gitRoot string, defaultBranch string, assignment MutationAssignment) error {
	if manifest.Policy.BranchRules.RequireWorktree != nil && *manifest.Policy.BranchRules.RequireWorktree && assignment.WorktreePath == gitRoot {
		return fmt.Errorf("project %q requires an isolated worktree", manifest.Key)
	}
	if manifest.Policy.BranchRules.RequireTaskBranch != nil && *manifest.Policy.BranchRules.RequireTaskBranch && assignment.BranchName == "" {
		return fmt.Errorf("project %q requires a task-owned branch", manifest.Key)
	}
	if manifest.Policy.BranchRules.AllowDefaultBranchMutation != nil && !*manifest.Policy.BranchRules.AllowDefaultBranchMutation && assignment.BranchName == defaultBranch {
		return fmt.Errorf("project %q cannot mutate the default branch directly", manifest.Key)
	}
	return nil
}
