package commands

import (
	"fmt"
	"strings"
	"time"

	corecompanions "odin-os/internal/core/companions"
	"odin-os/internal/runtime/projections"
)

type CompanionCommand struct {
	Name           string
	DelegateAction string
	Kind           string
	Key            string
	Title          string
	Objective      string
	Trigger        string
	AgentKey       string
	PortalTrack    string
	Surface        string
	Goal           string
	JSON           bool
}

type CompanionView struct {
	ID     int64  `json:"id"`
	Key    string `json:"key"`
	Title  string `json:"title"`
	Kind   string `json:"kind"`
	Status string `json:"status"`
}

type CompanionGetView struct {
	ID                  int64     `json:"id"`
	WorkspaceID         int64     `json:"workspace_id"`
	Key                 string    `json:"key"`
	Title               string    `json:"title"`
	Kind                string    `json:"kind"`
	Charter             string    `json:"charter"`
	Status              string    `json:"status"`
	InitiativeScopeJSON string    `json:"initiative_scope_json"`
	ToolPolicyJSON      string    `json:"tool_policy_json"`
	MemoryPolicyJSON    string    `json:"memory_policy_json"`
	PlanningPolicyJSON  string    `json:"planning_policy_json"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

type CompanionTaskStateView struct {
	WorkspaceID          int64  `json:"workspace_id"`
	WorkspaceKey         string `json:"workspace_key"`
	CompanionKey         string `json:"companion_key"`
	OwnedInitiativeCount int    `json:"owned_initiative_count"`
	OpenWorkItemCount    int    `json:"open_work_item_count"`
	ActiveRunCount       int    `json:"active_run_count"`
	PendingApprovalCount int    `json:"pending_approval_count"`
	BlockedWorkItemCount int    `json:"blocked_work_item_count"`
	OverdueFollowUpCount int    `json:"overdue_follow_up_count"`
}

type CompanionStateView struct {
	ID        int64                            `json:"id"`
	Key       string                           `json:"key"`
	Title     string                           `json:"title"`
	Kind      string                           `json:"kind"`
	Status    string                           `json:"status"`
	TaskState CompanionTaskStateView           `json:"task_state"`
	Swarms    []projections.CompanionSwarmView `json:"swarms"`
}

type CompanionToolPolicyView struct {
	Allow []string `json:"allow"`
}

type CompanionMemoryPolicyView struct {
	Mode string `json:"mode"`
}

type CompanionPlanningPolicyView struct {
	Mode  string                      `json:"mode,omitempty"`
	Swarm *CompanionPlanningSwarmView `json:"swarm,omitempty"`
}

type CompanionPlanningSwarmView struct {
	MaxChildren int `json:"max_children,omitempty"`
}

type CompanionCapabilitiesView struct {
	ID             int64                       `json:"id"`
	Key            string                      `json:"key"`
	Title          string                      `json:"title"`
	Kind           string                      `json:"kind"`
	Status         string                      `json:"status"`
	ToolPolicy     CompanionToolPolicyView     `json:"tool_policy"`
	MemoryPolicy   CompanionMemoryPolicyView   `json:"memory_policy"`
	PlanningPolicy CompanionPlanningPolicyView `json:"planning_policy"`
}

type CompanionListView struct {
	Companions []CompanionView `json:"companions"`
}

type CompanionRunView struct {
	CompanionKey          string         `json:"companion_key"`
	Objective             string         `json:"objective"`
	RequestedSwarmTrigger string         `json:"requested_swarm_trigger,omitempty"`
	Task                  TaskCreateView `json:"task"`
}

type CompanionDelegationRunView struct {
	CompanionKey        string                    `json:"companion_key"`
	AgentKey            string                    `json:"agent_key"`
	PortalTrack         string                    `json:"portal_track"`
	Surface             string                    `json:"surface"`
	Goal                string                    `json:"goal,omitempty"`
	ParentTask          TaskCreateView            `json:"parent_task"`
	ParentRun           *RunView                  `json:"parent_run,omitempty"`
	ChildDelegations    []CompanionDelegationView `json:"child_delegations"`
	LearningProposalIDs []int64                   `json:"learning_proposal_ids,omitempty"`
}

type CompanionDelegationView struct {
	ID            int64  `json:"id"`
	DelegationKey string `json:"delegation_key"`
	Role          string `json:"role"`
	Status        string `json:"status"`
	ParentTaskID  int64  `json:"parent_task_id"`
	ParentRunID   *int64 `json:"parent_run_id,omitempty"`
	ChildTaskID   *int64 `json:"child_task_id,omitempty"`
	ChildRunID    *int64 `json:"child_run_id,omitempty"`
	Executor      string `json:"executor"`
	ArtifactCount int    `json:"artifact_count,omitempty"`
	DetailsJSON   string `json:"details_json,omitempty"`
}

type CompanionDelegationListView struct {
	Delegations []CompanionDelegationView `json:"delegations"`
}

type CompanionDelegationDetailView struct {
	Delegation CompanionDelegationView       `json:"delegation"`
	Artifacts  []CompanionDelegationArtifact `json:"artifacts"`
}

type CompanionDelegationArtifact struct {
	ID           int64     `json:"id"`
	DelegationID int64     `json:"delegation_id"`
	ArtifactType string    `json:"artifact_type"`
	Summary      string    `json:"summary"`
	DetailsJSON  string    `json:"details_json"`
	CreatedAt    time.Time `json:"created_at"`
}

func ParseCompanion(args []string) (CompanionCommand, error) {
	if len(args) == 0 {
		return CompanionCommand{}, fmt.Errorf(companionUsage)
	}

	command := CompanionCommand{Name: strings.ToLower(args[0])}
	switch command.Name {
	case "create", "list", "get", "state", "capabilities", "run", "delegate":
	default:
		return CompanionCommand{}, fmt.Errorf("unsupported companion subcommand: %s", args[0])
	}

	seenPositionalKey := false
	for index := 1; index < len(args); index++ {
		switch args[index] {
		case "--kind":
			if index+1 >= len(args) {
				return CompanionCommand{}, fmt.Errorf("--kind requires a value")
			}
			index++
			command.Kind = strings.ToLower(strings.TrimSpace(args[index]))
		case "--key":
			if index+1 >= len(args) {
				return CompanionCommand{}, fmt.Errorf("--key requires a value")
			}
			index++
			command.Key = strings.TrimSpace(args[index])
		case "--title":
			if index+1 >= len(args) {
				return CompanionCommand{}, fmt.Errorf("--title requires a value")
			}
			index++
			command.Title = strings.TrimSpace(args[index])
		case "--objective":
			if index+1 >= len(args) {
				return CompanionCommand{}, fmt.Errorf("--objective requires a value")
			}
			index++
			command.Objective = strings.TrimSpace(args[index])
		case "--trigger":
			if index+1 >= len(args) {
				return CompanionCommand{}, fmt.Errorf("--trigger requires a value")
			}
			index++
			command.Trigger = strings.ToLower(strings.TrimSpace(args[index]))
		case "--agent":
			if index+1 >= len(args) {
				return CompanionCommand{}, fmt.Errorf("--agent requires a value")
			}
			index++
			command.AgentKey = strings.TrimSpace(args[index])
		case "--portal-track":
			if index+1 >= len(args) {
				return CompanionCommand{}, fmt.Errorf("--portal-track requires a value")
			}
			index++
			command.PortalTrack = strings.TrimSpace(args[index])
		case "--surface":
			if index+1 >= len(args) {
				return CompanionCommand{}, fmt.Errorf("--surface requires a value")
			}
			index++
			command.Surface = strings.TrimSpace(args[index])
		case "--goal":
			if index+1 >= len(args) {
				return CompanionCommand{}, fmt.Errorf("--goal requires a value")
			}
			index++
			command.Goal = strings.TrimSpace(args[index])
		case "--json":
			if command.JSON {
				return CompanionCommand{}, fmt.Errorf("duplicate --json flag")
			}
			command.JSON = true
		default:
			if strings.HasPrefix(args[index], "--") {
				return CompanionCommand{}, fmt.Errorf("unknown companion argument: %s", args[index])
			}
			if command.Name == "delegate" && command.DelegateAction == "" {
				delegateAction := strings.ToLower(strings.TrimSpace(args[index]))
				if delegateAction == "list" || delegateAction == "show" {
					command.DelegateAction = delegateAction
					continue
				}
			}
			if command.Name == "create" || command.Name == "list" {
				return CompanionCommand{}, fmt.Errorf("unknown companion argument: %s", args[index])
			}
			if seenPositionalKey || command.Key != "" {
				return CompanionCommand{}, fmt.Errorf("companion %s accepts a single key", command.Name)
			}
			seenPositionalKey = true
			command.Key = strings.TrimSpace(args[index])
		}
	}

	if command.Name == "get" || command.Name == "state" || command.Name == "capabilities" {
		if command.Key == "" {
			return CompanionCommand{}, fmt.Errorf("companion %s requires a key", command.Name)
		}
	}

	switch command.Name {
	case "create":
		if command.Kind == "" {
			return CompanionCommand{}, fmt.Errorf("--kind is required")
		}
		if !isSupportedCompanionKind(command.Kind) {
			return CompanionCommand{}, fmt.Errorf("unsupported companion kind: %s", command.Kind)
		}
		if command.Key == "" {
			return CompanionCommand{}, fmt.Errorf("--key is required")
		}
		if command.Title == "" {
			return CompanionCommand{}, fmt.Errorf("--title is required")
		}
	case "list":
		if command.Kind != "" {
			return CompanionCommand{}, fmt.Errorf("--kind is only valid for companion create")
		}
		if command.Key != "" || command.Title != "" || command.Objective != "" || command.Trigger != "" || command.AgentKey != "" || command.PortalTrack != "" || command.Surface != "" || command.Goal != "" {
			return CompanionCommand{}, fmt.Errorf("companion list does not accept run or create flags")
		}
	case "get", "state", "capabilities":
		if command.Kind != "" {
			return CompanionCommand{}, fmt.Errorf("companion %s does not accept --kind", command.Name)
		}
		if command.Title != "" {
			return CompanionCommand{}, fmt.Errorf("companion %s does not accept --title", command.Name)
		}
		if command.Objective != "" || command.Trigger != "" || command.AgentKey != "" || command.PortalTrack != "" || command.Surface != "" || command.Goal != "" {
			return CompanionCommand{}, fmt.Errorf("companion %s does not accept run flags", command.Name)
		}
	case "run":
		if command.Kind != "" {
			return CompanionCommand{}, fmt.Errorf("companion run does not accept --kind")
		}
		if command.Title != "" {
			return CompanionCommand{}, fmt.Errorf("companion run does not accept --title")
		}
		if command.Objective == "" {
			return CompanionCommand{}, fmt.Errorf("--objective is required")
		}
		if command.AgentKey != "" || command.PortalTrack != "" || command.Surface != "" || command.Goal != "" {
			return CompanionCommand{}, fmt.Errorf("companion run does not accept delegation flags")
		}
	case "delegate":
		if command.Kind != "" {
			return CompanionCommand{}, fmt.Errorf("companion delegate does not accept --kind")
		}
		if command.Title != "" || command.Objective != "" || command.Trigger != "" {
			return CompanionCommand{}, fmt.Errorf("companion delegate does not accept create or run flags")
		}
		switch command.DelegateAction {
		case "list":
			if command.Key != "" {
				return CompanionCommand{}, fmt.Errorf("companion delegate list does not accept an identifier")
			}
			if command.AgentKey != "" || command.PortalTrack != "" || command.Surface != "" || command.Goal != "" {
				return CompanionCommand{}, fmt.Errorf("companion delegate list does not accept create flags")
			}
			return command, nil
		case "show":
			if command.Key == "" {
				return CompanionCommand{}, fmt.Errorf("companion delegate show requires an id or key")
			}
			if command.AgentKey != "" || command.PortalTrack != "" || command.Surface != "" || command.Goal != "" {
				return CompanionCommand{}, fmt.Errorf("companion delegate show does not accept create flags")
			}
			return command, nil
		case "":
		default:
			return CompanionCommand{}, fmt.Errorf("unsupported companion delegate action: %s", command.DelegateAction)
		}
		if command.Key == "" {
			return CompanionCommand{}, fmt.Errorf("companion delegate requires a key")
		}
		if command.AgentKey == "" {
			return CompanionCommand{}, fmt.Errorf("--agent is required")
		}
		if command.PortalTrack == "" {
			return CompanionCommand{}, fmt.Errorf("--portal-track is required")
		}
		if command.Surface == "" {
			return CompanionCommand{}, fmt.Errorf("--surface is required")
		}
	}

	return command, nil
}

const companionUsage = "usage: odin companion <create|list> [--kind <kind>] [--key <key>] [--title <title>] [--json] | odin companion <get|state|capabilities> <key> [--json] | odin companion run <key> --objective <objective> [--trigger <trigger>] [--json] | odin companion delegate <key> --agent <agent-key> --portal-track <track> --surface <surface> [--goal <goal>] [--json] | odin companion delegate <list|show <id|key>> [--json]"

func isSupportedCompanionKind(kind string) bool {
	switch corecompanions.Kind(strings.ToLower(kind)) {
	case corecompanions.KindAssistant, corecompanions.KindAdvisor, corecompanions.KindOperator, corecompanions.KindSpecialist:
		return true
	default:
		return false
	}
}
