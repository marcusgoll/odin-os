package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"odin-os/internal/executors/contract"
)

type headlessExecutor struct{}

func NewHeadless() contract.Executor {
	return headlessExecutor{}
}

func (headlessExecutor) Key() string {
	return "codex_headless"
}

func (headlessExecutor) Class() contract.ExecutorClass {
	return contract.ExecutorClassPlanBackedCLI
}

func (headlessExecutor) Health(context.Context) (contract.HealthReport, error) {
	driverPath := codexDriverPath()
	if err := validateDriverPath(driverPath); err != nil {
		return contract.HealthReport{
			Status:    contract.HealthStatusDegraded,
			Details:   fmt.Sprintf("codex driver script unavailable: %v", err),
			CheckedAt: time.Now().UTC(),
		}, nil
	}
	return contract.HealthReport{
		Status:    contract.HealthStatusHealthy,
		Details:   fmt.Sprintf("codex driver script ready at %s", driverPath),
		CheckedAt: time.Now().UTC(),
	}, nil
}

func (headlessExecutor) Capabilities(context.Context) (contract.Capabilities, error) {
	return contract.Capabilities{
		ExecutorClass:        contract.ExecutorClassPlanBackedCLI,
		SupportsResume:       true,
		SupportsCancel:       true,
		SupportsTools:        true,
		SupportsCostEstimate: true,
		SupportsHeadlessPlan: true,
		TaskKinds: []contract.TaskKind{
			contract.TaskKindGeneral,
			contract.TaskKindPlan,
			contract.TaskKindBuild,
			contract.TaskKindReview,
			contract.TaskKindQA,
			contract.TaskKindResearch,
		},
		Scopes: []string{"global", "odin-core", "project", "new-project"},
	}, nil
}

func (headlessExecutor) RunTask(ctx context.Context, spec contract.TaskSpec) (contract.ExecutionResult, error) {
	return runWithDriver(ctx, spec)
}

func runWithDriver(ctx context.Context, spec contract.TaskSpec) (contract.ExecutionResult, error) {
	payload, err := json.Marshal(spec)
	if err != nil {
		return contract.ExecutionResult{}, err
	}

	driverPath := codexDriverPath()
	if err := validateDriverPath(driverPath); err != nil {
		return contract.ExecutionResult{}, fmt.Errorf("codex driver unavailable: %w", err)
	}

	cmd := exec.CommandContext(ctx, driverPath)
	cmd.Env = append(os.Environ(), "ODIN_CODEX_DRIVER_ACTION=run")
	cmd.Stdin = bytes.NewReader(payload)

	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			stderr := strings.TrimSpace(string(exitErr.Stderr))
			if stderr == "" {
				stderr = strings.TrimSpace(string(output))
			}
			return contract.ExecutionResult{}, fmt.Errorf("codex driver failed: %s", stderr)
		}
		return contract.ExecutionResult{}, err
	}

	var result struct {
		Status   string            `json:"status"`
		Output   string            `json:"output"`
		Metadata map[string]string `json:"metadata"`
	}
	if err := json.Unmarshal(output, &result); err != nil {
		return contract.ExecutionResult{}, fmt.Errorf("decode codex driver output: %w", err)
	}
	if result.Status == "" {
		result.Status = "completed"
	}
	if result.Metadata == nil {
		result.Metadata = map[string]string{}
	}
	if err := ensureArtifactMetadata(spec, output, result.Metadata); err != nil {
		return contract.ExecutionResult{}, err
	}
	return contract.ExecutionResult{
		Handle: contract.TaskHandle{
			ExecutorKey: "codex_headless",
			ExternalID:  spec.ID,
			Status:      result.Status,
		},
		Status:   result.Status,
		Output:   result.Output,
		Metadata: result.Metadata,
	}, nil
}

func (headlessExecutor) ResumeTask(ctx context.Context, handle contract.TaskHandle, packet contract.ResumePacket) (contract.ExecutionResult, error) {
	return contract.ExecutionResult{
		Handle: contract.TaskHandle{
			ExecutorKey: "codex_headless",
			ExternalID:  handle.ExternalID,
			Status:      "completed",
		},
		Status: "completed",
		Output: fmt.Sprintf("codex_headless resumed %s with %s", handle.ExternalID, packet.Summary),
		Metadata: map[string]string{
			"resume_kind": packet.Kind,
		},
	}, nil
}

func (headlessExecutor) CancelTask(context.Context, contract.TaskHandle) error {
	return nil
}

func (headlessExecutor) EstimateCost(context.Context, contract.TaskSpec) (contract.CostEstimate, error) {
	return contract.CostEstimate{
		InputTokens:  0,
		OutputTokens: 0,
		EstimatedUSD: 0,
		Currency:     "USD",
	}, nil
}

func codexDriverPath() string {
	if driverPath := strings.TrimSpace(os.Getenv("ODIN_CODEX_DRIVER")); driverPath != "" {
		return filepath.Clean(driverPath)
	}
	if cwd, err := os.Getwd(); err == nil {
		if driverPath, ok := findDriverUpward(cwd); ok {
			return driverPath
		}
	}
	if executable, err := os.Executable(); err == nil {
		if resolved, err := filepath.EvalSymlinks(executable); err == nil {
			executable = resolved
		}
		if driverPath, ok := findDriverUpward(filepath.Dir(executable)); ok {
			return driverPath
		}
	}
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return filepath.Join("scripts", "drivers", "codex-headless.sh")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", "scripts", "drivers", "codex-headless.sh"))
}

func ensureArtifactMetadata(spec contract.TaskSpec, payload []byte, metadata map[string]string) error {
	if strings.TrimSpace(metadata["artifacts_json"]) != "" || strings.TrimSpace(metadata["artifact_path"]) != "" {
		return nil
	}

	baseDir := strings.TrimSpace(spec.Metadata["worktree_path"])
	if baseDir == "" {
		baseDir = strings.TrimSpace(spec.Metadata["repo_root"])
	}
	if baseDir == "" {
		return nil
	}

	artifactPath, err := writeDriverArtifact(baseDir, spec.ID, payload)
	if err != nil {
		return err
	}

	metadata["artifact_path"] = artifactPath
	encoded, err := json.Marshal([]string{artifactPath})
	if err != nil {
		return err
	}
	metadata["artifacts_json"] = string(encoded)
	return nil
}

func findDriverUpward(start string) (string, bool) {
	if start == "" {
		return "", false
	}

	dir := filepath.Clean(start)
	for {
		driverPath := filepath.Join(dir, "scripts", "drivers", "codex-headless.sh")
		if info, err := os.Stat(driverPath); err == nil && !info.IsDir() {
			return driverPath, true
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

func validateDriverPath(driverPath string) error {
	info, err := os.Stat(driverPath)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("%s is a directory", driverPath)
	}
	if info.Mode()&0o111 == 0 {
		return fmt.Errorf("%s is not executable", driverPath)
	}
	return nil
}

func writeDriverArtifact(baseDir, taskID string, payload []byte) (string, error) {
	artifactDir := filepath.Join(baseDir, ".odin", "artifacts")
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		return "", err
	}

	artifactPath := filepath.Join(artifactDir, sanitizeArtifactName(taskID)+".json")
	if err := os.WriteFile(artifactPath, payload, 0o644); err != nil {
		return "", err
	}
	return artifactPath, nil
}

func sanitizeArtifactName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "codex-headless-run"
	}

	var builder strings.Builder
	lastDash := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			builder.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			builder.WriteByte('-')
			lastDash = true
		}
	}

	sanitized := strings.Trim(builder.String(), "-")
	if sanitized == "" {
		return "codex-headless-run"
	}
	return sanitized
}
