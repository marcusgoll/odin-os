package commands

import (
	"fmt"
	"strings"
	"time"

	corecompanions "odin-os/internal/core/companions"
	"odin-os/internal/runtime/projections"
)

type CompanionCommand struct {
	Name  string
	Kind  string
	Key   string
	Title string
	JSON  bool
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

func ParseCompanion(args []string) (CompanionCommand, error) {
	if len(args) == 0 {
		return CompanionCommand{}, fmt.Errorf("usage: odin companion <create|list> [--kind <kind>] [--key <key>] [--title <title>] [--json] | odin companion <get|state|capabilities> <key> [--json]")
	}

	command := CompanionCommand{Name: strings.ToLower(args[0])}
	switch command.Name {
	case "create", "list", "get", "state", "capabilities":
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
		case "--json":
			if command.JSON {
				return CompanionCommand{}, fmt.Errorf("duplicate --json flag")
			}
			command.JSON = true
		default:
			if strings.HasPrefix(args[index], "--") {
				return CompanionCommand{}, fmt.Errorf("unknown companion argument: %s", args[index])
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
		if command.Key != "" || command.Title != "" {
			return CompanionCommand{}, fmt.Errorf("companion list does not accept --key or --title")
		}
	case "get", "state", "capabilities":
		if command.Kind != "" {
			return CompanionCommand{}, fmt.Errorf("companion %s does not accept --kind", command.Name)
		}
		if command.Title != "" {
			return CompanionCommand{}, fmt.Errorf("companion %s does not accept --title", command.Name)
		}
	}

	return command, nil
}

func isSupportedCompanionKind(kind string) bool {
	switch corecompanions.Kind(strings.ToLower(kind)) {
	case corecompanions.KindAssistant, corecompanions.KindAdvisor, corecompanions.KindOperator, corecompanions.KindSpecialist:
		return true
	default:
		return false
	}
}
