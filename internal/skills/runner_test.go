package skills

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunRestrictedCommandUsesRepoRootCwd(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	cwdPath := filepath.Join(t.TempDir(), "cwd.txt")
	handlerPath := filepath.Join(service.RepoRoot, "scripts", "skills", "cwd-skill.sh")
	writeExecutable(t, handlerPath, "#!/usr/bin/env bash\ncat >/dev/null\npwd >\""+cwdPath+"\"\nprintf '%s\\n' '{\"status\":\"ok\",\"summary\":\"cwd ok\"}'\n")

	stdout, stderr, err := runRestrictedCommand(context.Background(), service.RepoRoot, restrictedSkillMetadata{
		Key:              "cwd-skill",
		Handler:          "scripts/skills/cwd-skill.sh",
		ExecutionProfile: restrictedSkillExecutionProfile,
	}, handlerPath, []byte("{}"))
	if err != nil {
		t.Fatalf("runRestrictedCommand() error = %v", err)
	}
	if strings.TrimSpace(stderr) != "" {
		t.Fatalf("runRestrictedCommand() stderr = %q, want empty", stderr)
	}
	if strings.TrimSpace(stdout) != "{\"status\":\"ok\",\"summary\":\"cwd ok\"}" {
		t.Fatalf("runRestrictedCommand() stdout = %q, want structured response", stdout)
	}

	got := strings.TrimSpace(mustReadFile(t, cwdPath))
	if got != service.RepoRoot {
		t.Fatalf("handler cwd = %q, want repo root %q", got, service.RepoRoot)
	}
}

func TestRunRestrictedCommandScrubsInheritedEnvironment(t *testing.T) {
	service := newTestService(t)
	leakPath := filepath.Join(t.TempDir(), "leak.txt")
	handlerPath := filepath.Join(service.RepoRoot, "scripts", "skills", "env-scrub-skill.sh")

	t.Setenv("ODIN_SHOULD_NOT_LEAK", "secret-value")
	writeExecutable(t, handlerPath, "#!/usr/bin/env bash\ncat >/dev/null\nprintf '%s' \"${ODIN_SHOULD_NOT_LEAK-}\" >\""+leakPath+"\"\nprintf '%s\\n' '{\"status\":\"ok\",\"summary\":\"env scrub ok\"}'\n")

	stdout, stderr, err := runRestrictedCommand(context.Background(), service.RepoRoot, restrictedSkillMetadata{
		Key:              "env-scrub-skill",
		Handler:          "scripts/skills/env-scrub-skill.sh",
		ExecutionProfile: restrictedSkillExecutionProfile,
	}, handlerPath, []byte("{}"))
	if err != nil {
		t.Fatalf("runRestrictedCommand() error = %v", err)
	}
	if strings.TrimSpace(stderr) != "" {
		t.Fatalf("runRestrictedCommand() stderr = %q, want empty", stderr)
	}
	if strings.TrimSpace(stdout) != "{\"status\":\"ok\",\"summary\":\"env scrub ok\"}" {
		t.Fatalf("runRestrictedCommand() stdout = %q, want structured response", stdout)
	}

	if got := mustReadFile(t, leakPath); got != "" {
		t.Fatalf("handler observed leaked env = %q, want empty", got)
	}
}

func TestRunRestrictedCommandPreservesPathForEnvBash(t *testing.T) {
	service := newTestService(t)
	handlerPath := filepath.Join(service.RepoRoot, "scripts", "skills", "path-skill.sh")

	realBash, err := exec.LookPath("bash")
	if err != nil {
		t.Fatalf("LookPath(\"bash\") error = %v", err)
	}
	binDir := t.TempDir()
	if err := os.Symlink(realBash, filepath.Join(binDir, "bash")); err != nil {
		t.Fatalf("Symlink(%q, %q) error = %v", realBash, filepath.Join(binDir, "bash"), err)
	}
	t.Setenv("PATH", binDir)

	writeExecutable(t, handlerPath, "#!/usr/bin/env bash\nprintf '%s\\n' '{\"status\":\"ok\",\"summary\":\"path ok\"}'\n")

	stdout, stderr, err := runRestrictedCommand(context.Background(), service.RepoRoot, restrictedSkillMetadata{
		Key:              "path-skill",
		Handler:          "scripts/skills/path-skill.sh",
		ExecutionProfile: restrictedSkillExecutionProfile,
	}, handlerPath, []byte("{}"))
	if err != nil {
		t.Fatalf("runRestrictedCommand() error = %v", err)
	}
	if strings.TrimSpace(stderr) != "" {
		t.Fatalf("runRestrictedCommand() stderr = %q, want empty", stderr)
	}
	if strings.TrimSpace(stdout) != "{\"status\":\"ok\",\"summary\":\"path ok\"}" {
		t.Fatalf("runRestrictedCommand() stdout = %q, want structured response", stdout)
	}
}

func TestRunRestrictedCommandPreservesRequiredEnvVars(t *testing.T) {
	service := newTestService(t)
	handlerPath := filepath.Join(service.RepoRoot, "scripts", "skills", "metadata-skill.sh")
	metadataPath := filepath.Join(t.TempDir(), "metadata.txt")
	tmpDir := filepath.Join(t.TempDir(), "tmp")
	odinRoot := filepath.Join(t.TempDir(), "odin-root")
	handlerRef := filepath.ToSlash(filepath.Join("scripts", "skills", "metadata-skill.sh"))

	t.Setenv("TMPDIR", tmpDir)
	t.Setenv("ODIN_ROOT", odinRoot)
	t.Setenv("ODIN_SHOULD_NOT_LEAK", "secret-value")

	writeExecutable(t, handlerPath, `#!/usr/bin/env bash
cat >/dev/null
{
  printf 'PATH=%s\n' "${PATH-}"
  printf 'TMPDIR=%s\n' "${TMPDIR-}"
  printf 'ODIN_ROOT=%s\n' "${ODIN_ROOT-}"
  printf 'ODIN_SKILL_KEY=%s\n' "${ODIN_SKILL_KEY-}"
  printf 'ODIN_SKILL_HANDLER=%s\n' "${ODIN_SKILL_HANDLER-}"
  printf 'ODIN_SKILL_EXECUTION_PROFILE=%s\n' "${ODIN_SKILL_EXECUTION_PROFILE-}"
  printf 'ODIN_SHOULD_NOT_LEAK=%s\n' "${ODIN_SHOULD_NOT_LEAK-}"
} >"`+metadataPath+`"
printf '%s\n' '{"status":"ok","summary":"metadata ok"}'
`)

	stdout, stderr, err := runRestrictedCommand(context.Background(), service.RepoRoot, restrictedSkillMetadata{
		Key:              "metadata-skill",
		Handler:          handlerRef,
		ExecutionProfile: restrictedSkillExecutionProfile,
	}, handlerPath, []byte("{}"))
	if err != nil {
		t.Fatalf("runRestrictedCommand() error = %v", err)
	}
	if strings.TrimSpace(stderr) != "" {
		t.Fatalf("runRestrictedCommand() stderr = %q, want empty", stderr)
	}
	if strings.TrimSpace(stdout) != "{\"status\":\"ok\",\"summary\":\"metadata ok\"}" {
		t.Fatalf("runRestrictedCommand() stdout = %q, want structured response", stdout)
	}

	got := mustReadFile(t, metadataPath)
	if !strings.Contains(got, "PATH=") {
		t.Fatalf("metadata env missing PATH:\n%s", got)
	}
	if !strings.Contains(got, "TMPDIR="+tmpDir) {
		t.Fatalf("metadata env missing TMPDIR:\n%s", got)
	}
	if !strings.Contains(got, "ODIN_ROOT="+odinRoot) {
		t.Fatalf("metadata env missing ODIN_ROOT:\n%s", got)
	}
	if !strings.Contains(got, "ODIN_SKILL_KEY=metadata-skill") {
		t.Fatalf("metadata env missing skill key:\n%s", got)
	}
	if !strings.Contains(got, "ODIN_SKILL_HANDLER="+handlerRef) {
		t.Fatalf("metadata env missing handler path:\n%s", got)
	}
	if !strings.Contains(got, "ODIN_SKILL_EXECUTION_PROFILE="+restrictedSkillExecutionProfile) {
		t.Fatalf("metadata env missing execution profile:\n%s", got)
	}
	if !strings.Contains(got, "ODIN_SHOULD_NOT_LEAK=") {
		t.Fatalf("metadata env missing leak probe:\n%s", got)
	}
}

func TestRunRestrictedCommandHonorsTimeout(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	handlerPath := filepath.Join(service.RepoRoot, "scripts", "skills", "timeout-skill.sh")
	writeExecutable(t, handlerPath, "#!/usr/bin/env bash\nsleep 2\nprintf '%s\\n' '{\"status\":\"ok\",\"summary\":\"slow\"}'\n")

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, _, err := runRestrictedCommand(ctx, service.RepoRoot, restrictedSkillMetadata{
		Key:              "timeout-skill",
		Handler:          "scripts/skills/timeout-skill.sh",
		ExecutionProfile: restrictedSkillExecutionProfile,
	}, handlerPath, []byte("{}"))
	if err == nil {
		t.Fatal("runRestrictedCommand() error = nil, want timeout")
	}
	if !strings.Contains(err.Error(), "deadline") && !strings.Contains(err.Error(), "killed") {
		t.Fatalf("runRestrictedCommand() error = %v, want timeout-related failure", err)
	}
}

func TestRunRestrictedCommandKillsProcessGroupOnTimeout(t *testing.T) {
	service := newTestService(t)
	handlerPath := filepath.Join(service.RepoRoot, "scripts", "skills", "group-kill-skill.py")
	markerPath := filepath.Join(t.TempDir(), "child-marker.txt")

	script := fmt.Sprintf(`#!/usr/bin/env python3
import subprocess
import time

marker = %q
subprocess.Popen([
    "sh",
    "-c",
    f"sleep 0.3; printf '%%s\n' 'child survived' >{marker}",
])
time.sleep(2)
print('{"status":"ok","summary":"should not complete"}')
`, markerPath)
	writeExecutable(t, handlerPath, script)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, _, err := runRestrictedCommand(ctx, service.RepoRoot, restrictedSkillMetadata{
		Key:              "group-kill-skill",
		Handler:          "scripts/skills/group-kill-skill.py",
		ExecutionProfile: restrictedSkillExecutionProfile,
	}, handlerPath, []byte("{}"))
	if err == nil {
		t.Fatal("runRestrictedCommand() error = nil, want timeout")
	}

	time.Sleep(500 * time.Millisecond)

	if _, statErr := os.Stat(markerPath); !os.IsNotExist(statErr) {
		t.Fatalf("child marker = %v, want child process killed with wrapper", statErr)
	}
}
