package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	runtimeevents "odin-os/internal/runtime/events"
	"odin-os/internal/store/sqlite"
)

func TestSkillLifecycleCrudAndInvocation(t *testing.T) {
	t.Parallel()

	projectRepo := projectRoot(t)
	binaryPath := buildOdinBinary(t, projectRepo)
	fixtureRoot := createCLIRepoRootWithPreferredExecutor(t, "codex_headless")
	runtimeRoot := t.TempDir()

	scriptPath := filepath.Join(fixtureRoot, "scripts", "skills", "echo-skill.sh")
	if err := os.MkdirAll(filepath.Dir(scriptPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(script dir) error = %v", err)
	}
	if err := os.WriteFile(scriptPath, []byte(`#!/usr/bin/env bash
cat >/dev/null
printf '%s\n' '{"status":"ok","summary":"echo complete","output":{"message":"hello"}}'
`), 0o755); err != nil {
		t.Fatalf("WriteFile(script) error = %v", err)
	}

	createSpecPath := filepath.Join(fixtureRoot, "echo-skill.json")
	if err := os.WriteFile(createSpecPath, []byte(`{
  "key": "echo-skill",
  "title": "Echo Skill",
  "summary": "Echoes a structured response.",
  "status": "active",
  "version": "1.0.0",
  "enabled": true,
  "tags": ["testing"],
  "owners": ["odin-core"],
  "strictness": "rigid",
  "applies_to": ["testing"],
  "scopes": ["project"],
  "permissions": ["repo.read"],
  "handler_type": "command",
  "handler_ref": "scripts/skills/echo-skill.sh",
  "timeout_seconds": 15,
  "input_schema": {"type":"object"},
  "output_schema": {"type":"object"},
  "sections": {
    "Purpose": "Echo input.",
    "When to Use": "When testing.",
    "Inputs": "A message.",
    "Procedure": "Read and echo.",
    "Outputs": "A JSON response.",
    "Constraints": "Stay deterministic.",
    "Success Criteria": "The caller gets a stable response."
  }
}`), 0o644); err != nil {
		t.Fatalf("WriteFile(create spec) error = %v", err)
	}

	updateSpecPath := filepath.Join(fixtureRoot, "echo-skill-v2.json")
	if err := os.WriteFile(updateSpecPath, []byte(`{
  "key": "echo-skill",
  "title": "Echo Skill",
  "summary": "Updated summary.",
  "status": "active",
  "version": "1.0.1",
  "enabled": true,
  "tags": ["testing"],
  "owners": ["odin-core"],
  "strictness": "rigid",
  "applies_to": ["testing"],
  "scopes": ["project"],
  "permissions": ["repo.read"],
  "handler_type": "command",
  "handler_ref": "scripts/skills/echo-skill.sh",
  "timeout_seconds": 15,
  "input_schema": {"type":"object"},
  "output_schema": {"type":"object"},
  "sections": {
    "Purpose": "Echo input.",
    "When to Use": "When testing.",
    "Inputs": "A message.",
    "Procedure": "Read and echo.",
    "Outputs": "A JSON response.",
    "Constraints": "Stay deterministic.",
    "Success Criteria": "The caller gets a stable response."
  }
}`), 0o644); err != nil {
		t.Fatalf("WriteFile(update spec) error = %v", err)
	}

	stdout, stderr, err := runOdinCommandCaptured(t, fixtureRoot, binaryPath, runtimeRoot, nil, "", "skills", "create", "--spec", createSpecPath, "--json")
	if err != nil {
		t.Fatalf("odin skills create error = %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	created := decodeJSONOutput[tinySkillView](t, stdout)
	if created.Key != "echo-skill" {
		t.Fatalf("skills create key = %q, want echo-skill", created.Key)
	}
	if created.Version != "1.0.0" {
		t.Fatalf("skills create version = %q, want 1.0.0", created.Version)
	}

	stdout, stderr, err = runOdinCommandCaptured(t, fixtureRoot, binaryPath, runtimeRoot, nil, "", "skills", "list", "--json")
	if err != nil {
		t.Fatalf("odin skills list error = %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	listed := decodeJSONOutput[skillListView](t, stdout)
	if len(listed.Skills) != 1 || listed.Skills[0].Key != "echo-skill" {
		t.Fatalf("skills list payload = %+v, want echo-skill", listed)
	}

	stdout, stderr, err = runOdinCommandCaptured(t, fixtureRoot, binaryPath, runtimeRoot, nil, "", "skills", "invoke", "echo-skill", "--input", `{"message":"hello"}`, "--json")
	if err != nil {
		t.Fatalf("odin skills invoke error = %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	invoked := decodeJSONOutput[skillInvokeView](t, stdout)
	if invoked.SkillKey != "echo-skill" {
		t.Fatalf("skills invoke skill_key = %q, want echo-skill", invoked.SkillKey)
	}
	if invoked.Summary != "echo complete" {
		t.Fatalf("skills invoke summary = %q, want echo complete", invoked.Summary)
	}

	stdout, stderr, err = runOdinCommandCaptured(t, fixtureRoot, binaryPath, runtimeRoot, nil, "", "skills", "update", "echo-skill", "--spec", updateSpecPath, "--json")
	if err != nil {
		t.Fatalf("odin skills update error = %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	updated := decodeJSONOutput[tinySkillView](t, stdout)
	if updated.Version != "1.0.1" {
		t.Fatalf("skills update version = %q, want 1.0.1", updated.Version)
	}

	stdout, stderr, err = runOdinCommandCaptured(t, fixtureRoot, binaryPath, runtimeRoot, nil, "", "skills", "delete", "echo-skill", "--json")
	if err != nil {
		t.Fatalf("odin skills delete error = %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	deleted := decodeJSONOutput[skillDeleteView](t, stdout)
	if !deleted.Deleted {
		t.Fatalf("skills delete deleted = %v, want true", deleted.Deleted)
	}

	stdout, stderr, err = runOdinCommandCaptured(t, fixtureRoot, binaryPath, runtimeRoot, nil, "", "skills", "list", "--json")
	if err != nil {
		t.Fatalf("odin skills list after delete error = %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	listed = decodeJSONOutput[skillListView](t, stdout)
	if len(listed.Skills) != 0 {
		t.Fatalf("skills list after delete payload = %+v, want empty", listed)
	}

	store := openRuntimeStore(t, runtimeRoot)
	defer store.Close()

	events, err := store.ListEvents(context.Background(), sqlite.ListEventsParams{})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}

	var skillEvents []runtimeevents.Record
	for _, event := range events {
		if event.StreamType == runtimeevents.StreamSkill && event.Type == runtimeevents.EventSkillLifecycleRecorded {
			skillEvents = append(skillEvents, event)
		}
	}
	if len(skillEvents) < 6 {
		t.Fatalf("skill lifecycle event count = %d, want at least 6", len(skillEvents))
	}

	var operations []string
	for _, event := range skillEvents {
		payload, err := runtimeevents.DecodePayload[runtimeevents.SkillLifecycleRecordedPayload](event.Payload)
		if err != nil {
			t.Fatalf("DecodePayload(SkillLifecycleRecordedPayload) error = %v", err)
		}
		operations = append(operations, payload.Operation)
	}

	wantOperations := []string{"create", "list", "invoke", "update", "delete", "list"}
	if strings.Join(operations, ",") != strings.Join(wantOperations, ",") {
		t.Fatalf("skill lifecycle operations = %v, want %v", operations, wantOperations)
	}

	deletePayload, err := runtimeevents.DecodePayload[runtimeevents.SkillLifecycleRecordedPayload](skillEvents[4].Payload)
	if err != nil {
		t.Fatalf("DecodePayload(delete payload) error = %v", err)
	}
	if deletePayload.SkillKey != "echo-skill" {
		t.Fatalf("delete payload key = %q, want echo-skill", deletePayload.SkillKey)
	}
	if deletePayload.Outcome != "success" {
		t.Fatalf("delete payload outcome = %q, want success", deletePayload.Outcome)
	}
}

func TestSkillLifecycleDeniedInvokeRecordsPermissionCode(t *testing.T) {
	t.Parallel()

	projectRepo := projectRoot(t)
	binaryPath := buildOdinBinary(t, projectRepo)
	fixtureRoot := createCLIRepoRootWithPreferredExecutor(t, "codex_headless")
	runtimeRoot := t.TempDir()

	scriptPath := filepath.Join(fixtureRoot, "scripts", "skills", "mutating-skill.sh")
	if err := os.MkdirAll(filepath.Dir(scriptPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(script dir) error = %v", err)
	}
	if err := os.WriteFile(scriptPath, []byte(`#!/usr/bin/env bash
cat >/dev/null
printf '%s\n' '{"status":"ok","summary":"should not run"}'
`), 0o755); err != nil {
		t.Fatalf("WriteFile(script) error = %v", err)
	}

	createSpecPath := filepath.Join(fixtureRoot, "mutating-skill.json")
	if err := os.WriteFile(createSpecPath, []byte(`{
  "key": "mutating-skill",
  "title": "Mutating Skill",
  "summary": "Attempts a repo mutation.",
  "status": "active",
  "version": "1.0.0",
  "enabled": true,
  "tags": ["testing"],
  "owners": ["odin-core"],
  "strictness": "rigid",
  "applies_to": ["testing"],
  "scopes": ["project"],
  "permissions": ["repo.mutate.full"],
  "handler_type": "command",
  "handler_ref": "scripts/skills/mutating-skill.sh",
  "timeout_seconds": 15,
  "input_schema": {"type":"object"},
  "output_schema": {"type":"object"},
  "sections": {
    "Purpose": "Exercise permission denial.",
    "When to Use": "When testing.",
    "Inputs": "A request.",
    "Procedure": "Attempt invocation.",
    "Outputs": "A structured response.",
    "Constraints": "Must be denied outside project scope.",
    "Success Criteria": "The runtime records the denial."
  }
}`), 0o644); err != nil {
		t.Fatalf("WriteFile(create spec) error = %v", err)
	}

	stdout, stderr, err := runOdinCommandCaptured(t, fixtureRoot, binaryPath, runtimeRoot, nil, "", "skills", "create", "--spec", createSpecPath, "--json")
	if err != nil {
		t.Fatalf("odin skills create error = %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}

	stdout, stderr, err = runOdinCommandCaptured(t, fixtureRoot, binaryPath, runtimeRoot, nil, "", "skills", "invoke", "mutating-skill", "--input", `{"message":"hello"}`, "--json")
	if err == nil {
		t.Fatalf("odin skills invoke error = nil, want denial\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if stdout != "" {
		t.Fatalf("skills invoke stdout = %q, want empty on denial", stdout)
	}
	if !strings.Contains(stderr, "global scope") {
		t.Fatalf("skills invoke stderr = %q, want global scope denial", stderr)
	}

	store := openRuntimeStore(t, runtimeRoot)
	defer store.Close()

	events, err := store.ListEvents(context.Background(), sqlite.ListEventsParams{})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}

	var invokePayload *runtimeevents.SkillLifecycleRecordedPayload
	for _, event := range events {
		if event.StreamType != runtimeevents.StreamSkill {
			continue
		}
		payload, err := runtimeevents.DecodePayload[runtimeevents.SkillLifecycleRecordedPayload](event.Payload)
		if err != nil {
			t.Fatalf("DecodePayload(SkillLifecycleRecordedPayload) error = %v", err)
		}
		if payload.SkillKey == "mutating-skill" && payload.Operation == "invoke" {
			copied := payload
			invokePayload = &copied
		}
	}
	if invokePayload == nil {
		t.Fatal("invoke payload missing for mutating-skill")
	}
	if invokePayload.Outcome != "failure" {
		t.Fatalf("invoke payload outcome = %q, want failure", invokePayload.Outcome)
	}
	if invokePayload.ErrorCode != runtimeevents.SkillLifecycleErrorMutationRequiresProjectScope {
		t.Fatalf("invoke payload error code = %q, want %q", invokePayload.ErrorCode, runtimeevents.SkillLifecycleErrorMutationRequiresProjectScope)
	}
	if !strings.Contains(invokePayload.ErrorText, "global scope") {
		t.Fatalf("invoke payload error text = %q, want global scope denial", invokePayload.ErrorText)
	}
}

func TestSkillInvocationUsesRestrictedWrapperAndRejectsOutsideHandlers(t *testing.T) {
	projectRepo := projectRoot(t)
	binaryPath := buildOdinBinary(t, projectRepo)
	fixtureRoot := createCLIRepoRootWithPreferredExecutor(t, "codex_headless")
	runtimeRoot := t.TempDir()

	t.Setenv("ODIN_SHOULD_NOT_LEAK", "secret-value")

	cwdPath := filepath.Join(t.TempDir(), "skill-cwd.txt")
	leakPath := filepath.Join(t.TempDir(), "skill-env.txt")
	profilePath := filepath.Join(t.TempDir(), "skill-profile.txt")
	scriptPath := filepath.Join(fixtureRoot, "scripts", "skills", "wrapper-skill.sh")
	if err := os.MkdirAll(filepath.Dir(scriptPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(script dir) error = %v", err)
	}
	if err := os.WriteFile(scriptPath, []byte(fmt.Sprintf(`#!/usr/bin/env bash
set -euo pipefail
cat >/dev/null
pwd >%q
printf '%%s' "${ODIN_SHOULD_NOT_LEAK-}" >%q
printf '%%s' "${ODIN_SKILL_EXECUTION_PROFILE-}" >%q
printf '%%s\n' '{"status":"ok","summary":"wrapper complete","output":{"message":"wrapped"}}'
`, cwdPath, leakPath, profilePath)), 0o755); err != nil {
		t.Fatalf("WriteFile(script) error = %v", err)
	}

	createSpecPath := filepath.Join(fixtureRoot, "wrapper-skill.json")
	if err := os.WriteFile(createSpecPath, []byte(`{
  "key": "wrapper-skill",
  "title": "Wrapper Skill",
  "summary": "Proves the restricted wrapper.",
  "status": "active",
  "version": "1.0.0",
  "enabled": true,
  "tags": ["testing"],
  "owners": ["odin-core"],
  "strictness": "rigid",
  "applies_to": ["testing"],
  "scopes": ["project"],
  "permissions": ["repo.read"],
  "handler_type": "command",
  "handler_ref": "scripts/skills/wrapper-skill.sh",
  "timeout_seconds": 15,
  "input_schema": {"type":"object"},
  "output_schema": {"type":"object"},
  "sections": {
    "Purpose": "Prove the restricted wrapper.",
    "When to Use": "When validating end-to-end skill invocation.",
    "Inputs": "A message payload.",
    "Procedure": "Run through the restricted wrapper.",
    "Outputs": "A structured response.",
    "Constraints": "Stay within scripts/skills and scrub inherited env.",
    "Success Criteria": "The handler sees the repo root and a scrubbed environment."
  }
}`), 0o644); err != nil {
		t.Fatalf("WriteFile(create spec) error = %v", err)
	}

	stdout, stderr, err := runOdinCommandCaptured(t, fixtureRoot, binaryPath, runtimeRoot, nil, "", "skills", "create", "--spec", createSpecPath, "--json")
	if err != nil {
		t.Fatalf("odin skills create error = %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}

	stdout, stderr, err = runOdinCommandCaptured(t, fixtureRoot, binaryPath, runtimeRoot, nil, "", "skills", "invoke", "wrapper-skill", "--input", `{"message":"hello"}`, "--json")
	if err != nil {
		t.Fatalf("odin skills invoke error = %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	invoked := decodeJSONOutput[skillInvokeView](t, stdout)
	if invoked.SkillKey != "wrapper-skill" {
		t.Fatalf("skills invoke skill_key = %q, want wrapper-skill", invoked.SkillKey)
	}
	if invoked.Summary != "wrapper complete" {
		t.Fatalf("skills invoke summary = %q, want wrapper complete", invoked.Summary)
	}

	gotCwd, err := os.ReadFile(cwdPath)
	if err != nil {
		t.Fatalf("ReadFile(cwd) error = %v", err)
	}
	if strings.TrimSpace(string(gotCwd)) != fixtureRoot {
		t.Fatalf("handler cwd = %q, want repo root %q", strings.TrimSpace(string(gotCwd)), fixtureRoot)
	}

	gotLeak, err := os.ReadFile(leakPath)
	if err != nil {
		t.Fatalf("ReadFile(leak) error = %v", err)
	}
	if strings.TrimSpace(string(gotLeak)) != "" {
		t.Fatalf("handler observed leaked env = %q, want empty", strings.TrimSpace(string(gotLeak)))
	}

	gotProfile, err := os.ReadFile(profilePath)
	if err != nil {
		t.Fatalf("ReadFile(profile) error = %v", err)
	}
	if strings.TrimSpace(string(gotProfile)) != "restricted_command_v1" {
		t.Fatalf("handler observed execution profile = %q, want restricted_command_v1", strings.TrimSpace(string(gotProfile)))
	}

	store := openRuntimeStore(t, runtimeRoot)
	defer store.Close()

	events, err := store.ListEvents(context.Background(), sqlite.ListEventsParams{})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}

	var invokePayload *runtimeevents.SkillLifecycleRecordedPayload
	for _, event := range events {
		if event.StreamType != runtimeevents.StreamSkill {
			continue
		}
		payload, err := runtimeevents.DecodePayload[runtimeevents.SkillLifecycleRecordedPayload](event.Payload)
		if err != nil {
			t.Fatalf("DecodePayload(SkillLifecycleRecordedPayload) error = %v", err)
		}
		if payload.SkillKey == "wrapper-skill" && payload.Operation == "invoke" {
			copied := payload
			invokePayload = &copied
		}
	}
	if invokePayload == nil {
		t.Fatal("invoke payload missing for wrapper-skill")
	}
	if invokePayload.ExecutionProfile != "restricted_command_v1" {
		t.Fatalf("invoke payload execution profile = %q, want restricted_command_v1", invokePayload.ExecutionProfile)
	}
	if invokePayload.Outcome != "success" {
		t.Fatalf("invoke payload outcome = %q, want success", invokePayload.Outcome)
	}

	outsideHandlerPath := filepath.Join(fixtureRoot, "registry", "skills", "outside-handler.sh")
	if err := os.MkdirAll(filepath.Dir(outsideHandlerPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(outside handler dir) error = %v", err)
	}
	if err := os.WriteFile(outsideHandlerPath, []byte("#!/usr/bin/env bash\nprintf '%s\\n' '{\"status\":\"ok\",\"summary\":\"should not run\"}'\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(outside handler) error = %v", err)
	}

	outsideSpecPath := filepath.Join(fixtureRoot, "outside-handler.json")
	if err := os.WriteFile(outsideSpecPath, []byte(`{
  "key": "outside-handler",
  "title": "Outside Handler Skill",
  "summary": "Should be rejected because the handler is outside scripts/skills.",
  "status": "active",
  "version": "1.0.0",
  "enabled": true,
  "tags": ["testing"],
  "owners": ["odin-core"],
  "strictness": "rigid",
  "applies_to": ["testing"],
  "scopes": ["project"],
  "permissions": ["repo.read"],
  "handler_type": "command",
  "handler_ref": "registry/skills/outside-handler.sh",
  "timeout_seconds": 15,
  "input_schema": {"type":"object"},
  "output_schema": {"type":"object"},
  "sections": {
    "Purpose": "Prove handler allowlisting.",
    "When to Use": "When validating handler roots.",
    "Inputs": "A message payload.",
    "Procedure": "Attempt to register the handler.",
    "Outputs": "A structured response.",
    "Constraints": "The handler must stay under scripts/skills.",
    "Success Criteria": "The CLI rejects the handler."
  }
}`), 0o644); err != nil {
		t.Fatalf("WriteFile(outside spec) error = %v", err)
	}

	stdout, stderr, err = runOdinCommandCaptured(t, fixtureRoot, binaryPath, runtimeRoot, nil, "", "skills", "create", "--spec", outsideSpecPath, "--json")
	if err == nil {
		t.Fatalf("odin skills create outside handler error = nil, want rejection\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if stdout != "" {
		t.Fatalf("outside handler stdout = %q, want empty on rejection", stdout)
	}
	if !strings.Contains(stderr, "must stay under scripts/skills") {
		t.Fatalf("outside handler stderr = %q, want scripts/skills rejection", stderr)
	}
}

func TestSkillInvocationPermissionGateE2E(t *testing.T) {
	t.Parallel()

	projectRepo := projectRoot(t)
	binaryPath := buildOdinBinary(t, projectRepo)
	fixtureRoot := createCLIRepoRootWithPreferredExecutor(t, "codex_headless")
	runtimeRoot := t.TempDir()

	for _, script := range []struct {
		path    string
		content string
	}{
		{
			path: filepath.Join(fixtureRoot, "scripts", "skills", "read-only-skill.sh"),
			content: `#!/usr/bin/env bash
cat >/dev/null
printf '%s\n' '{"status":"ok","summary":"read-only complete"}'
`,
		},
		{
			path: filepath.Join(fixtureRoot, "scripts", "skills", "mutating-skill.sh"),
			content: `#!/usr/bin/env bash
cat >/dev/null
printf '%s\n' '{"status":"ok","summary":"mutating complete"}'
`,
		},
		{
			path: filepath.Join(fixtureRoot, "scripts", "skills", "isolated-skill.sh"),
			content: `#!/usr/bin/env bash
cat >/dev/null
printf '%s\n' '{"status":"ok","summary":"isolated complete"}'
`,
		},
	} {
		if err := os.MkdirAll(filepath.Dir(script.path), 0o755); err != nil {
			t.Fatalf("MkdirAll(script dir) error = %v", err)
		}
		if err := os.WriteFile(script.path, []byte(script.content), 0o755); err != nil {
			t.Fatalf("WriteFile(script) error = %v", err)
		}
	}

	for _, spec := range []struct {
		path    string
		content string
	}{
		{
			path: filepath.Join(fixtureRoot, "read-only-skill.json"),
			content: `{
  "key": "read-only-skill",
  "title": "Read Only Skill",
  "summary": "Read-only access.",
  "status": "active",
  "version": "1.0.0",
  "enabled": true,
  "tags": ["testing"],
  "owners": ["odin-core"],
  "strictness": "rigid",
  "applies_to": ["testing"],
  "scopes": ["global", "project"],
  "permissions": ["repo.read"],
  "handler_type": "command",
  "handler_ref": "scripts/skills/read-only-skill.sh",
  "timeout_seconds": 15,
  "input_schema": {"type":"object"},
  "output_schema": {"type":"object"},
  "sections": {
    "Purpose": "Read-only invoke.",
    "When to Use": "When testing.",
    "Inputs": "A request.",
    "Procedure": "Invoke safely.",
    "Outputs": "A structured response.",
    "Constraints": "Must stay read-only.",
    "Success Criteria": "Invocation succeeds in global scope."
  }
}`,
		},
		{
			path: filepath.Join(fixtureRoot, "mutating-skill.json"),
			content: `{
  "key": "mutating-skill",
  "title": "Mutating Skill",
  "summary": "Full mutation access.",
  "status": "active",
  "version": "1.0.0",
  "enabled": true,
  "tags": ["testing"],
  "owners": ["odin-core"],
  "strictness": "rigid",
  "applies_to": ["testing"],
  "scopes": ["project"],
  "permissions": ["repo.mutate.full"],
  "handler_type": "command",
  "handler_ref": "scripts/skills/mutating-skill.sh",
  "timeout_seconds": 15,
  "input_schema": {"type":"object"},
  "output_schema": {"type":"object"},
  "sections": {
    "Purpose": "Mutating invoke.",
    "When to Use": "When testing.",
    "Inputs": "A request.",
    "Procedure": "Attempt mutation.",
    "Outputs": "A structured response.",
    "Constraints": "Must be denied in global scope.",
    "Success Criteria": "Invocation is blocked."
  }
}`,
		},
		{
			path: filepath.Join(fixtureRoot, "isolated-skill.json"),
			content: `{
  "key": "isolated-skill",
  "title": "Isolated Skill",
  "summary": "Allowlisted isolated mutation.",
  "status": "active",
  "version": "1.0.0",
  "enabled": true,
  "tags": ["testing"],
  "owners": ["odin-core"],
  "strictness": "rigid",
  "applies_to": ["testing"],
  "scopes": ["project"],
  "permissions": ["repo.mutate.isolated:docs_audit_note"],
  "handler_type": "command",
  "handler_ref": "scripts/skills/isolated-skill.sh",
  "timeout_seconds": 15,
  "input_schema": {"type":"object"},
  "output_schema": {"type":"object"},
  "sections": {
    "Purpose": "Isolated invoke.",
    "When to Use": "When testing.",
    "Inputs": "A request.",
    "Procedure": "Invoke after allowlisting.",
    "Outputs": "A structured response.",
    "Constraints": "Must require a matching limited_action allowlist.",
    "Success Criteria": "Invocation succeeds after transition setup."
  }
}`,
		},
	} {
		if err := os.WriteFile(spec.path, []byte(spec.content), 0o644); err != nil {
			t.Fatalf("WriteFile(spec) error = %v", err)
		}
		stdout, stderr, err := runOdinCommandCaptured(t, fixtureRoot, binaryPath, runtimeRoot, nil, "", "skills", "create", "--spec", spec.path, "--json")
		if err != nil {
			t.Fatalf("odin skills create %s error = %v\nstdout:\n%s\nstderr:\n%s", spec.path, err, stdout, stderr)
		}
		if decodeJSONOutput[tinySkillView](t, stdout).Key == "" {
			t.Fatalf("odin skills create %s returned empty skill payload", spec.path)
		}
	}

	stdout, stderr, err := runOdinCommandCaptured(t, fixtureRoot, binaryPath, runtimeRoot, nil, "", "skills", "invoke", "read-only-skill", "--input", `{"message":"hello"}`, "--json")
	if err != nil {
		t.Fatalf("odin skills invoke read-only-skill error = %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if decodeJSONOutput[skillInvokeView](t, stdout).Summary != "read-only complete" {
		t.Fatalf("read-only invoke payload = %q, want read-only summary", stdout)
	}

	stdout, stderr, err = runOdinCommandCaptured(t, fixtureRoot, binaryPath, runtimeRoot, nil, "", "skills", "invoke", "mutating-skill", "--input", `{"message":"hello"}`, "--json")
	if err == nil {
		t.Fatalf("odin skills invoke mutating-skill error = nil, want denial\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if stdout != "" {
		t.Fatalf("mutating invoke stdout = %q, want empty on denial", stdout)
	}
	if !strings.Contains(stderr, "global scope") {
		t.Fatalf("mutating invoke stderr = %q, want global scope denial", stderr)
	}

	stdout, stderr, err = runOdinCommandCaptured(t, fixtureRoot, binaryPath, runtimeRoot, nil, "", "project", "select", "alpha-cli")
	if err != nil {
		t.Fatalf("odin project select alpha-cli error = %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}

	stdout, stderr, err = runOdinCommandCaptured(t, fixtureRoot, binaryPath, runtimeRoot, nil, "", "skills", "invoke", "isolated-skill", "--input", `{"message":"hello"}`, "--json")
	if err == nil {
		t.Fatalf("odin skills invoke isolated-skill before allowlisting error = nil, want denial\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if stdout != "" {
		t.Fatalf("isolated invoke pre-allowlist stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, "transition denied") {
		t.Fatalf("isolated invoke pre-allowlist stderr = %q, want transition denial", stderr)
	}

	stdout, stderr, err = runOdinCommandCaptured(t, fixtureRoot, binaryPath, runtimeRoot, nil, "", "transition", "set", "limited_action", "allow=docs_audit_note", "confirm", "because", "skill gate e2e")
	if err != nil {
		t.Fatalf("odin transition set limited_action (matching) error = %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}

	stdout, stderr, err = runOdinCommandCaptured(t, fixtureRoot, binaryPath, runtimeRoot, nil, "", "skills", "invoke", "isolated-skill", "--input", `{"message":"hello"}`, "--json")
	if err != nil {
		t.Fatalf("odin skills invoke isolated-skill error = %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if decodeJSONOutput[skillInvokeView](t, stdout).Summary != "isolated complete" {
		t.Fatalf("isolated invoke payload = %q, want isolated summary", stdout)
	}

	store := openRuntimeStore(t, runtimeRoot)
	defer store.Close()

	events, err := store.ListEvents(context.Background(), sqlite.ListEventsParams{})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}

	var readOnlyAllowed bool
	var globalDenied bool
	var isolatedDenied bool
	var isolatedAllowed bool
	for _, event := range events {
		if event.StreamType != runtimeevents.StreamSkill {
			continue
		}
		payload, err := runtimeevents.DecodePayload[runtimeevents.SkillLifecycleRecordedPayload](event.Payload)
		if err != nil {
			t.Fatalf("DecodePayload(SkillLifecycleRecordedPayload) error = %v", err)
		}
		if payload.Operation != "invoke" {
			continue
		}
		switch payload.SkillKey {
		case "read-only-skill":
			readOnlyAllowed = payload.Outcome == "success"
		case "mutating-skill":
			globalDenied = payload.Outcome == "failure" && payload.ErrorCode == runtimeevents.SkillLifecycleErrorMutationRequiresProjectScope
		case "isolated-skill":
			if payload.Outcome == "failure" {
				isolatedDenied = strings.Contains(payload.ErrorText, "transition denied")
			}
			if payload.Outcome == "success" {
				isolatedAllowed = true
			}
		}
	}

	if !readOnlyAllowed {
		t.Fatal("read-only invoke event missing successful outcome")
	}
	if !globalDenied {
		t.Fatal("mutating invoke event missing expected global-scope denial")
	}
	if !isolatedDenied {
		t.Fatal("isolated invoke event missing expected pre-allowlist denial")
	}
	if !isolatedAllowed {
		t.Fatal("isolated invoke event missing successful allowlisted outcome")
	}
}

func runOdinCommandCaptured(t *testing.T, repoRoot string, binaryPath string, runtimeRoot string, extraEnv map[string]string, stdin string, args ...string) (string, string, error) {
	t.Helper()

	cmd := exec.Command(binaryPath, args...)
	cmd.Dir = repoRoot
	if runtimeRoot != "" {
		if err := os.MkdirAll(runtimeRoot, 0o755); err != nil {
			t.Fatalf("MkdirAll(runtimeRoot) error = %v", err)
		}
	}

	env := append([]string{}, os.Environ()...)
	if runtimeRoot != "" {
		env = append(env, "ODIN_ROOT="+runtimeRoot)
	}
	for key, value := range extraEnv {
		env = append(env, key+"="+value)
	}
	cmd.Env = env
	cmd.Stdin = bytes.NewBufferString(stdin)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

func decodeJSONOutput[T any](t *testing.T, stdout string) T {
	t.Helper()

	var decoded T
	if err := json.Unmarshal([]byte(stdout), &decoded); err != nil {
		t.Fatalf("json.Unmarshal(stdout) error = %v\nstdout:\n%s", err, stdout)
	}
	return decoded
}

type tinySkillView struct {
	Key     string `json:"key"`
	Version string `json:"version"`
}

type skillListView struct {
	Skills []tinySkillView `json:"skills"`
}

type skillInvokeView struct {
	SkillKey string         `json:"skill_key"`
	Status   string         `json:"status"`
	Summary  string         `json:"summary"`
	Output   map[string]any `json:"output"`
}

type skillDeleteView struct {
	Key     string `json:"key"`
	Deleted bool   `json:"deleted"`
}
