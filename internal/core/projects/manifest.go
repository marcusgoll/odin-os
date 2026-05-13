package projects

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const EnvOdinCoreGitRoot = "ODIN_CORE_GIT_ROOT"

type ProjectClass string

const (
	ProjectClassLocalGit     ProjectClass = "local_git_project"
	ProjectClassGitHubBacked ProjectClass = "github_backed_project"
	ProjectClassSystem       ProjectClass = "system_project"
)

type Config struct {
	Version  int           `yaml:"version"`
	Projects []Manifest    `yaml:"projects"`
	Cutover  CutoverConfig `yaml:"cutover"`
}

type Manifest struct {
	Key           string       `yaml:"key"`
	Name          string       `yaml:"name"`
	ProjectClass  ProjectClass `yaml:"project_class"`
	GitRoot       string       `yaml:"git_root"`
	DefaultBranch string       `yaml:"default_branch"`
	SystemProject bool         `yaml:"system_project"`
	GitHub        GitHub       `yaml:"github"`
	Scheduler     Scheduler    `yaml:"scheduler"`
	Policy        Policy       `yaml:"policy"`
	SourcePath    string       `yaml:"-"`
}

type Scheduler struct {
	MaxConcurrentRuns    int `yaml:"max_concurrent_runs"`
	MaxStartsPerCycle    int `yaml:"max_starts_per_cycle"`
	StalledRunRetryLimit int `yaml:"stalled_run_retry_limit"`
}

func (scheduler Scheduler) WithDefaults() Scheduler {
	if scheduler.MaxConcurrentRuns <= 0 {
		scheduler.MaxConcurrentRuns = 1
	}
	if scheduler.MaxStartsPerCycle <= 0 {
		scheduler.MaxStartsPerCycle = 1
	}
	if scheduler.StalledRunRetryLimit <= 0 {
		scheduler.StalledRunRetryLimit = 2
	}
	return scheduler
}

type GitHub struct {
	Repo string `yaml:"repo"`
}

type Policy struct {
	AllowedCommands       []string                     `yaml:"allowed_commands"`
	LimitedActions        map[string]LimitedActionRule `yaml:"limited_actions"`
	BranchRules           BranchRules                  `yaml:"branch_rules"`
	ApprovalGates         ApprovalGates                `yaml:"approval_gates"`
	MergePolicy           MergePolicy                  `yaml:"merge_policy"`
	DestructiveOperations DestructiveOperations        `yaml:"destructive_operations"`
}

type LimitedActionRule struct {
	Description  string   `yaml:"description"`
	PathPrefixes []string `yaml:"path_prefixes"`
	TargetPath   string   `yaml:"target_path"`
	ContentMode  string   `yaml:"content_mode"`
}

type BranchRules struct {
	ProtectedBranches          []string `yaml:"protected_branches"`
	RequireWorktree            *bool    `yaml:"require_worktree"`
	RequireTaskBranch          *bool    `yaml:"require_task_branch"`
	AllowDefaultBranchMutation *bool    `yaml:"allow_default_branch_mutation"`
}

type ApprovalGates struct {
	RequireForGovernanceChanges     *bool `yaml:"require_for_governance_changes"`
	RequireForDestructiveOperations *bool `yaml:"require_for_destructive_operations"`
	RequireForSystemProjectChanges  *bool `yaml:"require_for_system_project_changes"`
}

type MergePolicy struct {
	Mode                       string `yaml:"mode"`
	AllowDirectToDefaultBranch *bool  `yaml:"allow_direct_to_default_branch"`
}

type DestructiveOperations struct {
	AllowReset              *bool `yaml:"allow_reset"`
	AllowClean              *bool `yaml:"allow_clean"`
	AllowForcePush          *bool `yaml:"allow_force_push"`
	RequireExplicitApproval *bool `yaml:"require_explicit_approval"`
}

type CutoverConfig struct {
	PilotProjects []CutoverPilotProject `yaml:"pilot_projects"`
}

type CutoverPilotProject struct {
	Key                       string          `yaml:"key"`
	RuntimeOwner              string          `yaml:"runtime_owner"`
	PrimaryController         string          `yaml:"primary_controller"`
	Stage                     TransitionState `yaml:"stage"`
	ComparisonContext         string          `yaml:"comparison_context"`
	LegacyPrimaryRequired     bool            `yaml:"legacy_primary_required"`
	ShadowGraduation          []string        `yaml:"shadow_graduation"`
	LimitedActionGraduation   []string        `yaml:"limited_action_graduation"`
	CutoverGraduation         []string        `yaml:"cutover_graduation"`
	LegacyDutiesToRetireOrder []string        `yaml:"legacy_duties_to_retire_in_order"`
}

func LoadManifestFile(path string) (Config, error) {
	var cfg Config

	content, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	if err := yaml.Unmarshal(content, &cfg); err != nil {
		return Config{}, err
	}

	baseDir := filepath.Dir(path)
	for index := range cfg.Projects {
		cfg.Projects[index].SourcePath = path
		cfg.Projects[index].GitRoot = resolveGitRoot(baseDir, cfg.Projects[index].GitRoot)
		applyOdinCoreGitRootOverride(&cfg.Projects[index])
	}

	return cfg, nil
}

func LoadManifestFiles(paths ...string) (Config, error) {
	if len(paths) == 0 {
		return Config{}, fmt.Errorf("at least one manifest path is required")
	}

	merged := Config{}
	for _, manifestPath := range paths {
		cfg, err := LoadManifestFile(manifestPath)
		if err != nil {
			return Config{}, err
		}
		if merged.Version == 0 {
			merged.Version = cfg.Version
		}
		merged.Projects = append(merged.Projects, cfg.Projects...)
		merged.Cutover.PilotProjects = append(merged.Cutover.PilotProjects, cfg.Cutover.PilotProjects...)
	}

	return merged, nil
}

func (cfg Config) CutoverPilotProject(key string) (CutoverPilotProject, bool) {
	return cfg.Cutover.PilotProject(key)
}

func (cutover CutoverConfig) PilotProject(key string) (CutoverPilotProject, bool) {
	for _, project := range cutover.PilotProjects {
		if project.Key == key {
			return project, true
		}
	}
	return CutoverPilotProject{}, false
}

func resolveGitRoot(baseDir, gitRoot string) string {
	if gitRoot == "" || filepath.IsAbs(gitRoot) {
		return gitRoot
	}
	return filepath.Clean(filepath.Join(baseDir, gitRoot))
}

func applyOdinCoreGitRootOverride(project *Manifest) {
	if project == nil || project.Key != odinCoreKey {
		return
	}
	if project.ProjectClass != ProjectClassSystem && !project.SystemProject {
		return
	}
	override := strings.TrimSpace(os.Getenv(EnvOdinCoreGitRoot))
	if override == "" {
		return
	}
	project.GitRoot = override
}
