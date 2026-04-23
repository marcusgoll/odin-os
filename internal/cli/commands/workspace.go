package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/sys/unix"
	coreprojects "odin-os/internal/core/projects"
	coreworkspace "odin-os/internal/core/workspace"
	"odin-os/internal/runtime/checkpoints"
	"odin-os/internal/store/sqlite"
)

type WorkspaceCommand struct {
	Action            string
	ProjectKey        string
	JSON              bool
	NoAttach          bool
	Force             bool
	ForceAttach       bool
	Objective         string
	TaskTarget        string
	LastCompletedStep string
	NextSteps         []string
	Constraints       []string
	Evidence          []checkpoints.Evidence
}

var (
	workspaceIsInteractiveTerminal = func() bool {
		_, err := unix.IoctlGetTermios(int(os.Stdin.Fd()), unix.TCGETS)
		return err == nil
	}
	workspaceCommandGetenv           = os.Getenv
	workspaceCommandStdin  io.Reader = os.Stdin
)

func ParseWorkspace(args []string) (WorkspaceCommand, error) {
	if len(args) == 0 {
		return parseWorkspaceList(nil)
	}

	switch args[0] {
	case "list":
		return parseWorkspaceList(args[1:])
	case "start":
		return parseWorkspaceTargetCommand("start", args[1:], false, false)
	case "status":
		return parseWorkspaceTargetCommand("status", args[1:], true, false)
	case "stop":
		return parseWorkspaceTargetCommand("stop", args[1:], false, true)
	case "attach":
		return parseWorkspaceTargetCommand("attach", args[1:], false, false)
	case "handoff":
		return parseWorkspaceHandoff(args[1:])
	default:
		return WorkspaceCommand{}, fmt.Errorf("unknown workspace command: %s", args[0])
	}
}

func parseWorkspaceList(args []string) (WorkspaceCommand, error) {
	command := WorkspaceCommand{Action: "list"}
	for _, token := range args {
		if token == "--json" {
			command.JSON = true
			continue
		}
		return WorkspaceCommand{}, fmt.Errorf("unknown workspace option: %s", token)
	}
	return command, nil
}

func parseWorkspaceHandoff(args []string) (WorkspaceCommand, error) {
	command := WorkspaceCommand{Action: "handoff"}
	index := 0
	if len(args) > 0 && !strings.Contains(args[0], "=") && args[0] != "--json" {
		command.ProjectKey = strings.TrimSpace(args[0])
		index = 1
	}

	for _, token := range args[index:] {
		if token == "--json" {
			command.JSON = true
			continue
		}

		key, value, ok := strings.Cut(token, "=")
		if !ok {
			return WorkspaceCommand{}, fmt.Errorf("unknown workspace option: %s", token)
		}
		value = strings.TrimSpace(value)
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "objective":
			command.Objective = value
		case "task":
			command.TaskTarget = value
		case "last_completed_step":
			command.LastCompletedStep = value
		case "next_steps":
			command.NextSteps = splitCSV(value)
		case "constraints":
			command.Constraints = splitCSV(value)
		case "evidence":
			items := splitCSV(value)
			command.Evidence = make([]checkpoints.Evidence, 0, len(items))
			for _, item := range items {
				command.Evidence = append(command.Evidence, checkpoints.Evidence{Kind: "note", Summary: item})
			}
		default:
			return WorkspaceCommand{}, fmt.Errorf("unknown workspace option: %s", token)
		}
	}

	if command.JSON {
		return command, nil
	}
	if strings.TrimSpace(command.Objective) == "" {
		return WorkspaceCommand{}, fmt.Errorf("workspace handoff requires objective=<value>")
	}
	return command, nil
}

func parseWorkspaceTargetCommand(action string, args []string, allowJSON bool, allowForce bool) (WorkspaceCommand, error) {
	command := WorkspaceCommand{Action: action}

	index := 0
	if len(args) > 0 && !strings.HasPrefix(args[0], "--") {
		command.ProjectKey = strings.TrimSpace(args[0])
		index = 1
	}

	for _, token := range args[index:] {
		switch token {
		case "--json":
			if !allowJSON {
				return WorkspaceCommand{}, fmt.Errorf("--json is only supported for workspace status")
			}
			command.JSON = true
		case "--no-attach":
			if action != "start" {
				return WorkspaceCommand{}, fmt.Errorf("--no-attach is only supported for workspace start")
			}
			command.NoAttach = true
		case "--force":
			if !allowForce {
				return WorkspaceCommand{}, fmt.Errorf("--force is only supported for workspace stop")
			}
			command.Force = true
		case "--force-attach":
			if action != "start" {
				return WorkspaceCommand{}, fmt.Errorf("--force-attach is only supported for workspace start")
			}
			command.ForceAttach = true
		default:
			return WorkspaceCommand{}, fmt.Errorf("unknown workspace option: %s", token)
		}
	}

	return command, nil
}

func RunWorkspace(ctx context.Context, store *sqlite.Store, registry coreprojects.Registry, args []string, stdout io.Writer) error {
	if store == nil {
		return fmt.Errorf("workspace store is required")
	}

	command, err := ParseWorkspace(args)
	if err != nil {
		return err
	}

	service := coreworkspace.Service{
		Store:    store,
		Registry: registry,
	}

	switch command.Action {
	case "list":
		statuses, err := service.List(ctx)
		if err != nil {
			return err
		}
		if command.JSON {
			return renderWorkspaceJSON(stdout, statuses)
		}
		return renderWorkspaceList(stdout, statuses)
	case "start":
		status, err := service.Start(ctx, command.ProjectKey)
		if err != nil {
			return err
		}
		return handleWorkspaceStart(ctx, service, command, status, stdout)
	case "status":
		status, err := service.Status(ctx, command.ProjectKey)
		if err != nil {
			return err
		}
		if command.JSON {
			return renderWorkspaceJSON(stdout, status)
		}
		return renderWorkspaceText(stdout, status)
	case "stop":
		status, err := service.Stop(ctx, command.ProjectKey, command.Force)
		if err != nil {
			return err
		}
		return renderWorkspaceText(stdout, status)
	case "attach":
		status, err := service.Attach(ctx, command.ProjectKey)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(stdout, "project=%s attached=%s state=%s\n", status.ProjectKey, status.SessionName, status.State)
		return err
	case "handoff":
		if command.JSON {
			if err := loadWorkspaceHandoffJSON(&command); err != nil {
				return err
			}
		}
		result, err := service.Handoff(ctx, command.ProjectKey, coreworkspace.HandoffRequest{
			Objective:         command.Objective,
			TaskTarget:        command.TaskTarget,
			LastCompletedStep: command.LastCompletedStep,
			NextSteps:         append([]string(nil), command.NextSteps...),
			Constraints:       append([]string(nil), command.Constraints...),
			Evidence:          append([]checkpoints.Evidence(nil), command.Evidence...),
		})
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(stdout, "project=%s task=%s trigger=handoff state=%s\n", result.Workspace.ProjectKey, result.Task.Key, result.Task.Status)
		return err
	default:
		return fmt.Errorf("unknown workspace action: %s", command.Action)
	}
}

func renderWorkspaceList(stdout io.Writer, statuses []coreworkspace.Status) error {
	for _, status := range statuses {
		if _, err := fmt.Fprintf(stdout, "project=%s session=%s state=%s branch=%s\n", status.ProjectKey, status.SessionName, status.State, status.Branch); err != nil {
			return err
		}
	}
	return nil
}

func handleWorkspaceStart(ctx context.Context, service coreworkspace.Service, command WorkspaceCommand, status coreworkspace.Status, stdout io.Writer) error {
	if shouldAutoAttachWorkspace(command) {
		attachedStatus, err := service.Attach(ctx, command.ProjectKey)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(stdout, "project=%s attached=%s state=%s\n", attachedStatus.ProjectKey, attachedStatus.SessionName, attachedStatus.State)
		return err
	}

	reason := workspaceStartSkipReason(command)
	if err := renderWorkspaceText(stdout, status); err != nil {
		return err
	}
	if reason != "" {
		if _, err := fmt.Fprintf(stdout, "attach_skipped=%s\n", reason); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(stdout, "attach_command=tmux attach-session -t %s\n", status.SessionName); err != nil {
		return err
	}
	return nil
}

func renderWorkspaceText(stdout io.Writer, status coreworkspace.Status) error {
	if _, err := fmt.Fprintf(
		stdout,
		"project=%s session=%s state=%s branch=%s dirty=%t transition=%s cwd=%s\n",
		status.ProjectKey,
		status.SessionName,
		status.State,
		status.Branch,
		status.Dirty,
		status.TransitionState,
		firstNonEmpty(status.CurrentCwd, status.LaunchCwd, status.GitRoot),
	); err != nil {
		return err
	}
	if status.FactsSource != "" && status.FactsSource != coreworkspace.FactsSourceLive {
		if _, err := fmt.Fprintf(stdout, "facts_source=%s\n", status.FactsSource); err != nil {
			return err
		}
	}
	if !status.WorkspaceEligible && strings.TrimSpace(status.WorkspaceReason) != "" {
		if _, err := fmt.Fprintf(stdout, "workspace_reason=%s\n", status.WorkspaceReason); err != nil {
			return err
		}
	}
	return nil
}

func renderWorkspaceJSON(stdout io.Writer, value any) error {
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func splitCSV(value string) []string {
	raw := strings.Split(value, ",")
	parts := make([]string, 0, len(raw))
	for _, item := range raw {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		parts = append(parts, trimmed)
	}
	return parts
}

func loadWorkspaceHandoffJSON(command *WorkspaceCommand) error {
	if command == nil {
		return fmt.Errorf("workspace handoff command is required")
	}

	type jsonPayload struct {
		Objective         string                 `json:"objective"`
		TaskTarget        string                 `json:"task"`
		LastCompletedStep string                 `json:"last_completed_step"`
		NextSteps         []string               `json:"next_steps"`
		Constraints       []string               `json:"constraints"`
		Evidence          []checkpoints.Evidence `json:"evidence"`
	}

	payloadBytes, err := io.ReadAll(workspaceCommandStdin)
	if err != nil {
		return err
	}
	var payload jsonPayload
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return fmt.Errorf("decode workspace handoff json: %w", err)
	}
	command.Objective = strings.TrimSpace(payload.Objective)
	command.TaskTarget = strings.TrimSpace(payload.TaskTarget)
	command.LastCompletedStep = strings.TrimSpace(payload.LastCompletedStep)
	command.NextSteps = append([]string(nil), payload.NextSteps...)
	command.Constraints = append([]string(nil), payload.Constraints...)
	command.Evidence = append([]checkpoints.Evidence(nil), payload.Evidence...)
	if command.Objective == "" {
		return fmt.Errorf("workspace handoff requires objective")
	}
	return nil
}

func shouldAutoAttachWorkspace(command WorkspaceCommand) bool {
	if command.NoAttach {
		return false
	}
	if !workspaceIsInteractiveTerminal() {
		return false
	}
	if workspaceCommandGetenv("TMUX") != "" && !command.ForceAttach {
		return false
	}
	return true
}

func workspaceStartSkipReason(command WorkspaceCommand) string {
	switch {
	case command.NoAttach:
		return "disabled"
	case !workspaceIsInteractiveTerminal():
		return "non_interactive"
	case workspaceCommandGetenv("TMUX") != "" && !command.ForceAttach:
		return "nested_tmux"
	default:
		return ""
	}
}
