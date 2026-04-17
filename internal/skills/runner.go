package skills

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

const restrictedSkillExecutionProfile = "restricted_command_v1"

type restrictedSkillMetadata struct {
	Key              string
	Handler          string
	ExecutionProfile string
}

func runRestrictedCommand(ctx context.Context, repoRoot string, metadata restrictedSkillMetadata, handlerPath string, payload []byte) (string, string, error) {
	cmd := exec.CommandContext(ctx, handlerPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
			return err
		}
		return nil
	}
	cmd.Dir = repoRoot
	cmd.Env = restrictedSkillEnv(metadata)
	cmd.Stdin = bytes.NewReader(payload)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

func restrictedSkillEnv(metadata restrictedSkillMetadata) []string {
	env := make([]string, 0, 5)

	path := strings.TrimSpace(os.Getenv("PATH"))
	if path == "" {
		path = "/usr/bin:/bin"
	}
	env = append(env, "PATH="+path)

	if tmpDir, ok := os.LookupEnv("TMPDIR"); ok {
		env = append(env, "TMPDIR="+tmpDir)
	}
	if odinRoot, ok := os.LookupEnv("ODIN_ROOT"); ok {
		env = append(env, "ODIN_ROOT="+odinRoot)
	}

	env = append(env, "ODIN_SKILL_KEY="+metadata.Key)
	env = append(env, "ODIN_SKILL_HANDLER="+skillHandlerEnvValue(metadata.Handler))
	env = append(env, "ODIN_SKILL_EXECUTION_PROFILE="+executionProfileEnvValue(metadata.ExecutionProfile))

	return env
}

func skillHandlerEnvValue(handler string) string {
	handler = strings.TrimSpace(handler)
	if handler == "" {
		return ""
	}
	return filepath.Clean(handler)
}

func executionProfileEnvValue(profile string) string {
	profile = strings.TrimSpace(profile)
	if profile == "" {
		return restrictedSkillExecutionProfile
	}
	return profile
}
