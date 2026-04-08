package projects

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type ProjectClass string

const (
	ProjectClassLocalGit     ProjectClass = "local_git_project"
	ProjectClassGitHubBacked ProjectClass = "github_backed_project"
	ProjectClassSystem       ProjectClass = "system_project"
)

type Config struct {
	Version  int        `yaml:"version"`
	Projects []Manifest `yaml:"projects"`
}

type Manifest struct {
	Key           string       `yaml:"key"`
	Name          string       `yaml:"name"`
	ProjectClass  ProjectClass `yaml:"project_class"`
	GitRoot       string       `yaml:"git_root"`
	DefaultBranch string       `yaml:"default_branch"`
	SystemProject bool         `yaml:"system_project"`
	GitHub        GitHub       `yaml:"github"`
	Policy        Policy       `yaml:"policy"`
	SourcePath    string       `yaml:"-"`
}

type GitHub struct {
	Repo string `yaml:"repo"`
}

type Policy struct {
	AllowedCommands       []string              `yaml:"allowed_commands"`
	BranchRules           BranchRules           `yaml:"branch_rules"`
	ApprovalGates         ApprovalGates         `yaml:"approval_gates"`
	MergePolicy           MergePolicy           `yaml:"merge_policy"`
	DestructiveOperations DestructiveOperations `yaml:"destructive_operations"`
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
	}

	return cfg, nil
}

func resolveGitRoot(baseDir, gitRoot string) string {
	if gitRoot == "" || filepath.IsAbs(gitRoot) {
		return gitRoot
	}
	return filepath.Clean(filepath.Join(baseDir, gitRoot))
}
