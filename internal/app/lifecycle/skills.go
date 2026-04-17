package lifecycle

import (
	"context"
	"fmt"
	"io"
	"os"

	"odin-os/internal/app/bootstrap"
	"odin-os/internal/cli/commands"
	"odin-os/internal/cli/scope"
	clistate "odin-os/internal/cli/state"
	"odin-os/internal/core/projects"
	"odin-os/internal/skills"
	"odin-os/internal/store/sqlite"
	"odin-os/internal/telemetry/logs"
)

func runSkills(ctx context.Context, app bootstrap.App, args []string, stdout io.Writer) error {
	jsonOutput, remaining, err := consumeJSONFlag(args)
	if err != nil {
		return err
	}
	if len(remaining) == 0 {
		return fmt.Errorf("usage: odin skills [list|get|create|update|delete|invoke] ... [--json]")
	}

	logger := logs.Logger{Writer: os.Stderr}
	observers := skills.MultiObserver{
		skills.LoggerObserver{
			Logger: logger,
			Scope:  "repo",
		},
	}
	if app.Store != nil {
		observers = append(observers, skills.ObserverFunc(func(ctx context.Context, event skills.Event) {
			if err := app.Store.RecordSkillLifecycleEvent(ctx, sqlite.RecordSkillLifecycleEventParams{
				SkillKey:         event.SkillKey,
				Scope:            event.Scope,
				Operation:        string(event.Operation),
				Outcome:          string(event.Outcome),
				ExecutionProfile: event.ExecutionProfile,
				Version:          event.Version,
				HandlerType:      event.HandlerType,
				HandlerRef:       event.HandlerRef,
				Permissions:      append([]string(nil), event.Permissions...),
				DurationMS:       event.Duration.Milliseconds(),
				ErrorCode:        event.ErrorCode,
				ErrorText:        event.ErrorText,
			}); err != nil {
				_ = logger.Log(logs.Record{
					Level:     logs.LevelWarn,
					Component: "skills",
					Message:   "skill lifecycle audit append failed",
					Scope: func() string {
						if event.Scope != "" {
							return event.Scope
						}
						return "repo"
					}(),
					Fields: map[string]any{
						"skill_key":         event.SkillKey,
						"operation":         event.Operation,
						"outcome":           event.Outcome,
						"execution_profile": event.ExecutionProfile,
						"error":             err.Error(),
						"error_code":        "skill_audit_append_failed",
					},
				})
			}
		}))
	}

	service := skills.Service{
		RepoRoot:             app.RepoRoot,
		Observer:             observers,
		TransitionAuthorizer: projects.Service{Store: app.Store},
	}

	state, err := loadCLIState(app)
	if err != nil {
		return err
	}

	invocationContext := skills.InvocationContext{
		ResolvedScopeKind: string(state.Scope.Kind),
	}

	switch remaining[0] {
	case "list":
		if len(remaining) != 1 {
			return fmt.Errorf("usage: odin skills list [--json]")
		}
		skillList, err := service.List(ctx)
		if err != nil {
			return err
		}
		if jsonOutput {
			return commands.WriteJSON(stdout, commands.SkillsView{Skills: skillList})
		}
		for _, skill := range skillList {
			if _, err := fmt.Fprintf(stdout, "%s %s\n", skill.Key, skill.Version); err != nil {
				return err
			}
		}
		return nil
	case "get":
		if len(remaining) != 2 {
			return fmt.Errorf("usage: odin skills get <key> [--json]")
		}
		skill, err := service.Get(ctx, remaining[1])
		if err != nil {
			return err
		}
		if jsonOutput {
			return commands.WriteJSON(stdout, skill)
		}
		_, err = fmt.Fprintf(stdout, "key=%s version=%s handler=%s\n", skill.Key, skill.Version, skill.HandlerRef)
		return err
	case "create":
		specPath, err := consumeFlagValue(remaining[1:], "--spec")
		if err != nil {
			return err
		}
		spec, err := commands.LoadSkillSpecFile(specPath)
		if err != nil {
			return err
		}
		skill, err := service.Create(ctx, spec)
		if err != nil {
			return err
		}
		if jsonOutput {
			return commands.WriteJSON(stdout, skill)
		}
		_, err = fmt.Fprintf(stdout, "created=%s version=%s\n", skill.Key, skill.Version)
		return err
	case "update":
		if len(remaining) < 2 {
			return fmt.Errorf("usage: odin skills update <key> --spec <path> [--json]")
		}
		specPath, err := consumeFlagValue(remaining[2:], "--spec")
		if err != nil {
			return err
		}
		spec, err := commands.LoadSkillSpecFile(specPath)
		if err != nil {
			return err
		}
		skill, err := service.Update(ctx, remaining[1], spec)
		if err != nil {
			return err
		}
		if jsonOutput {
			return commands.WriteJSON(stdout, skill)
		}
		_, err = fmt.Fprintf(stdout, "updated=%s version=%s\n", skill.Key, skill.Version)
		return err
	case "delete":
		if len(remaining) != 2 {
			return fmt.Errorf("usage: odin skills delete <key> [--json]")
		}
		if err := service.Delete(ctx, remaining[1]); err != nil {
			return err
		}
		if jsonOutput {
			return commands.WriteJSON(stdout, commands.SkillDeleteView{Key: remaining[1], Deleted: true})
		}
		_, err := fmt.Fprintf(stdout, "deleted=%s\n", remaining[1])
		return err
	case "invoke":
		if len(remaining) < 2 {
			return fmt.Errorf("usage: odin skills invoke <key> [--input <json>] [--json]")
		}
		inputValue, err := optionalFlagValue(remaining[2:], "--input")
		if err != nil {
			return err
		}
		input, err := commands.DecodeSkillInput(inputValue)
		if err != nil {
			return err
		}
		if state.Scope.Kind == scope.ScopeProject || state.Scope.Kind == scope.ScopeOdinCore {
			manifest, ok := app.Registry.Lookup(state.Scope.ProjectKey)
			if !ok {
				return fmt.Errorf("unknown project: %s", state.Scope.ProjectKey)
			}

			project, err := projects.Service{Store: app.Store}.RegisterManagedProject(ctx, manifest)
			if err != nil {
				return err
			}

			invocationContext.Project = &skills.InvocationProject{
				ID:            project.ID,
				Key:           project.Key,
				SystemProject: manifest.SystemProject,
			}
			invocationContext.Manifest = manifest
		}
		response, err := service.Invoke(ctx, skills.InvokeRequest{
			Key:     remaining[1],
			Input:   input,
			Context: invocationContext,
		})
		if err != nil {
			return err
		}
		if jsonOutput {
			return commands.WriteJSON(stdout, response)
		}
		_, err = fmt.Fprintf(stdout, "skill=%s status=%s summary=%s\n", response.SkillKey, response.Status, response.Summary)
		return err
	default:
		return fmt.Errorf("unknown skills subcommand: %s", remaining[0])
	}
}

func consumeFlagValue(args []string, flag string) (string, error) {
	value, err := optionalFlagValue(args, flag)
	if err != nil {
		return "", err
	}
	if value == "" {
		return "", fmt.Errorf("%s is required", flag)
	}
	return value, nil
}

func optionalFlagValue(args []string, flag string) (string, error) {
	var value string
	for index := 0; index < len(args); index++ {
		if args[index] != flag {
			continue
		}
		if value != "" {
			return "", fmt.Errorf("duplicate %s flag", flag)
		}
		if index+1 >= len(args) {
			return "", fmt.Errorf("%s requires a value", flag)
		}
		value = args[index+1]
		index++
	}
	return value, nil
}

func consumeJSONFlag(args []string) (bool, []string, error) {
	jsonOutput := false
	remaining := make([]string, 0, len(args))
	for _, arg := range args {
		if arg != "--json" {
			remaining = append(remaining, arg)
			continue
		}
		if jsonOutput {
			return false, nil, fmt.Errorf("duplicate --json flag")
		}
		jsonOutput = true
	}
	return jsonOutput, remaining, nil
}

func loadCLIState(app bootstrap.App) (clistate.State, error) {
	cache, err := app.SessionStore.Load()
	if err != nil {
		return clistate.State{}, err
	}
	return clistate.ResolveStartupState(cache, app.Registry), nil
}
