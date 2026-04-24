package commands

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	coreprojects "odin-os/internal/core/projects"
	"odin-os/internal/store/sqlite"
)

type ProjectCommand struct {
	Action string
	Key    string
	JSON   bool

	Input            coreprojects.ManagedProjectInput
	NameSet          bool
	GitRootSet       bool
	DefaultBranchSet bool
	ClassSet         bool
	GitHubRepoSet    bool
}

type projectView struct {
	Key                  string                      `json:"key"`
	Name                 string                      `json:"name"`
	ProjectClass         coreprojects.ProjectClass   `json:"project_class"`
	GitRoot              string                      `json:"git_root"`
	DefaultBranch        string                      `json:"default_branch"`
	GitHubRepo           string                      `json:"github_repo,omitempty"`
	ManifestPath         string                      `json:"manifest_path"`
	TransitionState      string                      `json:"transition_state"`
	TransitionController string                      `json:"transition_controller"`
	WorkspaceEligible    bool                        `json:"workspace_eligible"`
	WorkspaceReason      string                      `json:"workspace_reason,omitempty"`
	Profile              coreprojects.ProjectProfile `json:"profile"`
}

func ParseProject(args []string) (ProjectCommand, error) {
	if len(args) == 0 || args[0] == "list" {
		cmd := ProjectCommand{Action: "list"}
		for _, arg := range args[1:] {
			if arg == "--json" {
				cmd.JSON = true
				continue
			}
			return ProjectCommand{}, fmt.Errorf("unknown project flag: %s", arg)
		}
		return cmd, nil
	}

	switch args[0] {
	case "show":
		if len(args) < 2 {
			return ProjectCommand{}, fmt.Errorf("project show requires a project key")
		}
		cmd := ProjectCommand{Action: "show", Key: strings.TrimSpace(args[1])}
		for _, arg := range args[2:] {
			if arg == "--json" {
				cmd.JSON = true
				continue
			}
			return ProjectCommand{}, fmt.Errorf("unknown project flag: %s", arg)
		}
		if cmd.Key == "" {
			return ProjectCommand{}, fmt.Errorf("project key is required")
		}
		return cmd, nil
	case "enroll":
		return parseProjectMutation("enroll", args[1:], false)
	case "update":
		return parseProjectMutation("update", args[1:], true)
	default:
		return ProjectCommand{}, fmt.Errorf("unknown project command: %s", args[0])
	}
}

func RunProject(ctx context.Context, store *sqlite.Store, registry coreprojects.Registry, args []string, stdout io.Writer) error {
	if store == nil {
		return fmt.Errorf("project store is required")
	}

	command, err := ParseProject(args)
	if err != nil {
		return err
	}

	switch command.Action {
	case "list":
		views, err := listProjectViews(ctx, store, registry)
		if err != nil {
			return err
		}
		if command.JSON {
			return renderProjectJSON(stdout, views)
		}
		return renderProjectList(stdout, views)
	case "show":
		manifest, ok := registry.Lookup(command.Key)
		if !ok {
			return fmt.Errorf("unknown project: %s", command.Key)
		}
		view, err := buildProjectView(ctx, store, manifest)
		if err != nil {
			return err
		}
		if command.JSON {
			return renderProjectJSON(stdout, view)
		}
		return renderProjectShow(stdout, view)
	case "enroll":
		return runProjectEnroll(ctx, registry, command, stdout)
	case "update":
		return runProjectUpdate(ctx, registry, command, stdout)
	default:
		return fmt.Errorf("unknown project action: %s", command.Action)
	}
}

func parseProjectMutation(action string, args []string, requireKey bool) (ProjectCommand, error) {
	cmd := ProjectCommand{Action: action}
	if len(args) == 0 && requireKey {
		return ProjectCommand{}, fmt.Errorf("project update requires a project key")
	}

	index := 0
	if len(args) > 0 && !strings.Contains(args[0], "=") && args[0] != "--json" {
		cmd.Key = strings.TrimSpace(args[0])
		index = 1
	}
	if requireKey && cmd.Key == "" {
		return ProjectCommand{}, fmt.Errorf("project update requires a project key")
	}

	for _, token := range args[index:] {
		if token == "--json" {
			return ProjectCommand{}, fmt.Errorf("--json is only supported for project list and project show")
		}
		key, value, ok := strings.Cut(token, "=")
		if !ok {
			return ProjectCommand{}, fmt.Errorf("unknown project option: %s", token)
		}
		value = strings.TrimSpace(value)
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "name":
			cmd.Input.Name = value
			cmd.NameSet = true
		case "git_root":
			cmd.Input.GitRoot = value
			cmd.GitRootSet = true
		case "class":
			cmd.Input.ProjectClass = coreprojects.ProjectClass(value)
			cmd.ClassSet = true
		case "default_branch":
			cmd.Input.DefaultBranch = value
			cmd.DefaultBranchSet = true
		case "github_repo":
			cmd.Input.GitHubRepo = value
			cmd.GitHubRepoSet = true
		default:
			return ProjectCommand{}, fmt.Errorf("unknown project option: %s", token)
		}
	}

	return cmd, nil
}

func runProjectEnroll(ctx context.Context, registry coreprojects.Registry, command ProjectCommand, stdout io.Writer) error {
	input := command.Input
	needsHints := strings.TrimSpace(input.GitRoot) == "" || strings.TrimSpace(input.DefaultBranch) == ""
	if needsHints {
		hints, err := loadProjectRepoHints(ctx)
		if err != nil {
			return err
		}
		if strings.TrimSpace(input.GitRoot) == "" {
			input.GitRoot = hints.GitRoot
		}
		if strings.TrimSpace(input.DefaultBranch) == "" {
			input.DefaultBranch = hints.DefaultBranch
		}
	}
	if strings.TrimSpace(command.Key) == "" {
		command.Key = coreprojects.DeriveProjectKey(input.GitRoot)
	}
	input.Key = command.Key
	if strings.TrimSpace(input.Name) == "" {
		input.Name = command.Key
	}

	if strings.TrimSpace(input.Key) == "" {
		return fmt.Errorf("project key is required")
	}
	if strings.TrimSpace(input.GitRoot) == "" {
		return fmt.Errorf("git root is required")
	}
	if strings.TrimSpace(input.DefaultBranch) == "" {
		return fmt.Errorf("default branch is required")
	}
	if _, exists := registry.Lookup(input.Key); exists {
		return fmt.Errorf("project key %q already exists; use `odin project update %s` or pass a different key", input.Key, input.Key)
	}

	manifest := input.Manifest()
	updatedRegistry, diagnostics, err := coreprojects.AppendProject(registry.ConfigPath(), manifest)
	if err != nil {
		return err
	}
	if len(diagnostics) != 0 {
		return fmt.Errorf("unable to enroll project: %s", diagnostics[0].Message)
	}

	project, ok := updatedRegistry.Lookup(manifest.Key)
	if !ok {
		return fmt.Errorf("enrolled project but could not reload %s", manifest.Key)
	}
	_, err = fmt.Fprintf(stdout, "project=%s class=%s git_root=%s default_branch=%s\n", project.Key, project.ProjectClass, project.GitRoot, project.DefaultBranch)
	return err
}

func runProjectUpdate(ctx context.Context, registry coreprojects.Registry, command ProjectCommand, stdout io.Writer) error {
	if _, ok := registry.Lookup(command.Key); !ok {
		return fmt.Errorf("unknown project: %s", command.Key)
	}

	hints, hintsErr := loadProjectRepoHints(ctx)
	useHints := hintsErr == nil && hints.GitRoot != ""
	if !command.NameSet && !command.GitRootSet && !command.DefaultBranchSet && !command.ClassSet && !command.GitHubRepoSet && !useHints {
		return fmt.Errorf("project update requires at least one field or a current git checkout to infer from")
	}

	updatedRegistry, diagnostics, err := coreprojects.UpdateProject(registry.ConfigPath(), command.Key, func(manifest *coreprojects.Manifest) error {
		if command.NameSet {
			manifest.Name = strings.TrimSpace(command.Input.Name)
		}
		if command.ClassSet {
			manifest.ProjectClass = command.Input.ProjectClass
		}
		if command.GitHubRepoSet {
			manifest.GitHub.Repo = strings.TrimSpace(command.Input.GitHubRepo)
		}
		if command.GitRootSet {
			manifest.GitRoot = strings.TrimSpace(command.Input.GitRoot)
		} else if useHints {
			manifest.GitRoot = hints.GitRoot
		}
		if command.DefaultBranchSet {
			manifest.DefaultBranch = strings.TrimSpace(command.Input.DefaultBranch)
		} else if useHints {
			manifest.DefaultBranch = hints.DefaultBranch
		}
		if !command.ClassSet && command.GitHubRepoSet && strings.TrimSpace(manifest.GitHub.Repo) != "" && manifest.ProjectClass == coreprojects.ProjectClassLocalGit {
			manifest.ProjectClass = coreprojects.ProjectClassGitHubBacked
		}
		return nil
	})
	if err != nil {
		return err
	}
	if len(diagnostics) != 0 {
		return fmt.Errorf("unable to update project: %s", diagnostics[0].Message)
	}

	project, ok := updatedRegistry.Lookup(command.Key)
	if !ok {
		return fmt.Errorf("updated project but could not reload %s", command.Key)
	}
	_, err = fmt.Fprintf(stdout, "project=%s class=%s git_root=%s default_branch=%s\n", project.Key, project.ProjectClass, project.GitRoot, project.DefaultBranch)
	return err
}

func listProjectViews(ctx context.Context, store *sqlite.Store, registry coreprojects.Registry) ([]projectView, error) {
	manifests := registry.Projects()
	sort.Slice(manifests, func(i, j int) bool {
		return manifests[i].Key < manifests[j].Key
	})

	views := make([]projectView, 0, len(manifests))
	for _, manifest := range manifests {
		view, err := buildProjectView(ctx, store, manifest)
		if err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	return views, nil
}

func buildProjectView(ctx context.Context, store *sqlite.Store, manifest coreprojects.Manifest) (projectView, error) {
	state, controller, err := loadProjectTransition(ctx, store, manifest.Key)
	if err != nil {
		return projectView{}, err
	}
	eligible, reason := workspaceEligibility(manifest)

	return projectView{
		Key:                  manifest.Key,
		Name:                 manifest.Name,
		ProjectClass:         manifest.ProjectClass,
		GitRoot:              manifest.GitRoot,
		DefaultBranch:        manifest.DefaultBranch,
		GitHubRepo:           manifest.GitHub.Repo,
		ManifestPath:         manifest.SourcePath,
		TransitionState:      state,
		TransitionController: controller,
		WorkspaceEligible:    eligible,
		WorkspaceReason:      reason,
		Profile:              coreprojects.DetectProjectProfile(manifest.GitRoot),
	}, nil
}

func loadProjectTransition(ctx context.Context, store *sqlite.Store, key string) (string, string, error) {
	project, err := store.GetProjectByKey(ctx, key)
	if err != nil {
		if err == sql.ErrNoRows {
			return string(coreprojects.TransitionStateInventory), string(coreprojects.TransitionControllerLegacyOdin), nil
		}
		return "", "", err
	}

	transition, err := store.GetProjectTransition(ctx, project.ID)
	if err != nil {
		if err == sql.ErrNoRows {
			return string(coreprojects.TransitionStateInventory), string(coreprojects.TransitionControllerLegacyOdin), nil
		}
		return "", "", err
	}

	return transition.State, transition.Controller, nil
}

func workspaceEligibility(manifest coreprojects.Manifest) (bool, string) {
	if strings.TrimSpace(manifest.GitRoot) == "" {
		return false, "missing git_root"
	}
	if !coreprojects.IsGitRepository(manifest.GitRoot) {
		return false, "git_root is not a Git repository"
	}
	return true, ""
}

func loadProjectRepoHints(ctx context.Context) (coreprojects.ManagedProjectInput, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return coreprojects.ManagedProjectInput{}, err
	}

	gitRoot, err := coreprojects.InferGitRoot(ctx, cwd)
	if err != nil {
		return coreprojects.ManagedProjectInput{}, fmt.Errorf("unable to infer current git root: %w", err)
	}
	defaultBranch, err := coreprojects.InferDefaultBranch(ctx, gitRoot)
	if err != nil {
		return coreprojects.ManagedProjectInput{}, fmt.Errorf("unable to infer default branch: %w", err)
	}

	return coreprojects.ManagedProjectInput{
		Key:           coreprojects.DeriveProjectKey(gitRoot),
		GitRoot:       gitRoot,
		DefaultBranch: defaultBranch,
	}, nil
}

func renderProjectList(stdout io.Writer, views []projectView) error {
	for _, view := range views {
		workspace := "eligible"
		if !view.WorkspaceEligible {
			workspace = "ineligible"
		}
		if _, err := fmt.Fprintf(stdout, "project=%s class=%s transition=%s workspace=%s\n", view.Key, view.ProjectClass, view.TransitionState, workspace); err != nil {
			return err
		}
		if view.Profile.SpecFlowCompatible {
			if _, err := fmt.Fprintln(stdout, "profile=spec_flow_compatible"); err != nil {
				return err
			}
		}
		if !view.WorkspaceEligible {
			if _, err := fmt.Fprintf(stdout, "workspace_reason=%s\n", view.WorkspaceReason); err != nil {
				return err
			}
		}
	}
	return nil
}

func renderProjectShow(stdout io.Writer, view projectView) error {
	lines := []string{
		"key=" + view.Key,
		"name=" + view.Name,
		"project_class=" + string(view.ProjectClass),
		"git_root=" + view.GitRoot,
		"default_branch=" + view.DefaultBranch,
		"manifest_path=" + view.ManifestPath,
		"transition_state=" + view.TransitionState,
		"transition_controller=" + view.TransitionController,
		fmt.Sprintf("workspace_eligible=%t", view.WorkspaceEligible),
	}
	if view.Profile.SpecFlowCompatible {
		lines = append(lines, "spec_flow_compatible=true")
		if len(view.Profile.Evidence) > 0 {
			lines = append(lines, "spec_flow_evidence="+strings.Join(view.Profile.Evidence, ","))
		}
	}
	if view.GitHubRepo != "" {
		lines = append(lines, "github_repo="+view.GitHubRepo)
	}
	if view.WorkspaceReason != "" {
		lines = append(lines, "workspace_reason="+view.WorkspaceReason)
	}
	_, err := fmt.Fprintln(stdout, strings.Join(lines, "\n"))
	return err
}

func renderProjectJSON(stdout io.Writer, value any) error {
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}
