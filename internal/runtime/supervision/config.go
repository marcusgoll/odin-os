package supervision

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

var ErrInvalidConfig = errors.New("invalid_supervision_config")

type Config struct {
	ModeKey                 string   `json:"mode_key"`
	MaxConcurrentTasks      int      `json:"max_concurrent_tasks"`
	DryRun                  bool     `json:"dry_run"`
	RequireHumanApproval    bool     `json:"require_human_approval"`
	RequiredLabels          []string `json:"required_labels"`
	AllowedPathPrefixes     []string `json:"allowed_path_prefixes"`
	ForbiddenPathPrefixes   []string `json:"forbidden_path_prefixes"`
	SensitiveTestSubstrings []string `json:"sensitive_test_substrings"`
}

func DefaultConfig() Config {
	return Config{
		ModeKey:              ModeKeyStage7SupervisedAgency,
		MaxConcurrentTasks:   1,
		DryRun:               false,
		RequireHumanApproval: true,
		RequiredLabels:       []string{"odin:ready", "safety:low-risk"},
		AllowedPathPrefixes:  []string{"docs/", "prompts/", "fixtures/"},
		ForbiddenPathPrefixes: []string{
			".github/workflows/",
			"deploy/",
			"internal/runner/",
			"internal/security/",
			"internal/workspace/",
			"scripts/deploy/",
		},
		SensitiveTestSubstrings: []string{
			"auth",
			"dashboard",
			"deploy",
			"runner",
			"secret",
			"security",
			"token",
			"workspace",
		},
	}
}

func ConfigHash(config Config) (string, error) {
	stable := config
	stable.RequiredLabels = sortedCopy(stable.RequiredLabels)
	stable.AllowedPathPrefixes = sortedCopy(stable.AllowedPathPrefixes)
	stable.ForbiddenPathPrefixes = sortedCopy(stable.ForbiddenPathPrefixes)
	stable.SensitiveTestSubstrings = sortedCopy(stable.SensitiveTestSubstrings)

	raw, err := json.Marshal(stable)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func ValidateConfig(config Config) error {
	if config.MaxConcurrentTasks != 1 {
		return fmt.Errorf("%w: max_concurrent_tasks must be 1", ErrInvalidConfig)
	}
	if config.DryRun {
		return fmt.Errorf("%w: dry_run must be false", ErrInvalidConfig)
	}
	if !config.RequireHumanApproval {
		return fmt.Errorf("%w: require_human_approval must be true", ErrInvalidConfig)
	}
	return nil
}

func sortedCopy(values []string) []string {
	copied := append([]string(nil), values...)
	sort.Strings(copied)
	return copied
}
