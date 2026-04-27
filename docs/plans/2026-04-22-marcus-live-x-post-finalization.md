# Marcus Live X Post Finalization Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Finish the first Marcus-live X-post operator surface so the code, tests, contracts, operations docs, and safe real-`odin` proof all agree on one canonical global/workflow shell path.

**Domain Source of Truth:** `CONTEXT.md`, `docs/contracts/marcus-social-copilot.md`, `docs/contracts/live-driver-tools.md`, `docs/operations/marcus-live-x-post-runbook.md`, `docs/adr/0001-canonical-authority.md`, `docs/adr/0002-migration-policy.md`

**Context:** Marcus Social Copilot

**Owns / Does Not Own:** Owns the first Marcus-live X-post loop on the existing `./bin/odin` shell surface, including draft, approval, native X publish, visible evidence, and operator documentation. Does not own LinkedIn automation, live reply publishing, new first-class runtime social entities, or any new wrapper command family.

**Invariants:**
- The canonical term is **Social Copilot**, not “social media manager”.
- The first live operator surface stays on the existing repo-local `./bin/odin` shell path.
- The first live loop is global/workflow scoped and must not require a temporary project or `odin-core` scope switch for compose preflight.
- The canonical happy path is `social_draft -> /memory resolve -> /memory publish`, with the same `social_outcome` row updated in place.
- Native X publish remains operator-attended and explicit; LinkedIn remains manual; replies remain suggestion-only.
- Visible evidence is a separate required step recorded as `social_evidence`; publish screenshots on `social_outcome` are supporting artifacts only.
- Durable proof uses `/memory list` plus `/memory show`; transient outputs such as `tool_memory=<id>` are supporting only.
- Machine-local operator config stays in plain-assignment `~/.config/odin/odin.env`; runtime truth stays out of repo-owned mutable files per ADR 0001.

**Architecture:** Extend the existing tool catalog and shell surfaces instead of creating a parallel preflight command or wrapper. Keep durable policy in `docs/contracts/`, the exact operator sequence in `docs/operations/`, and execution proof on the real repo-local binary. Use doc alignment only where the current contracts still drift from the locked social operator model.

**Tech Stack:** Go CLI, SQLite-backed runtime, registry Markdown assets, shell package tests, repo-local browser driver scripts, Markdown contracts and operations docs

---

## Context Mapping

- **Context:** Marcus Social Copilot inside the Odin interactive shell
- **Owns:** social draft approval/publish/evidence operator flow, social preflight scope on the shell surface, social operator docs, social driver contract wording
- **Depends on:** tool catalog scope filtering, workflow and skill registry assets, `/memory` persistence, repo-local driver scripts, machine-local `~/.config/odin/odin.env`
- **Does not own:** LinkedIn publishing interfaces, reply publishing, service-mode orchestration, new persistence primitives, legacy `odin-orchestrator` assets
- **Boundary crossings:** tool catalog -> live driver commands via env vars; contracts -> operations runbook; interactive shell -> workflow-scoped memory rows; repo code -> operator-local `~/.config/odin/odin.env`

## Execution Notes

- Execute this plan in a clean worktree. The active repo may already contain partial local changes; verify current state before editing and skip only what is already proven in your execution branch.
- Use `@superpowers:test-driven-development` for code tasks and `@superpowers:verification-before-completion` before close-out.
- Keep commits scoped to each task. Do not commit `~/.config/odin/odin.env`.

### Task 1: Keep Compose Preflight On The Global Social Surface

**Domain Goal:** Make the compose-preflight tool available on the same canonical global/workflow operator surface as the rest of the first Marcus-live X-post loop.

**Domain Rules Enforced:**
- No temporary project or `odin-core` scope switch for social compose preflight
- Reuse the existing `huginn_visual_audit` tool instead of inventing a new social-specific command

**Why this matters:**
- The domain model already says the first live social loop stays on one shell surface. If the tool scope disagrees, the runbook becomes false.

**Files:**
- Modify: `internal/tools/catalog/builtin.go`
- Test: `internal/cli/repl/shell_test.go`

**Step 1: Write the failing test**

```go
func TestShellToolRunInvokesLiveVisualAuditToolInGlobalScope(t *testing.T) {
	ctx := context.Background()
	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	scriptPath := filepath.Join(t.TempDir(), "visual-driver.sh")
	script := `#!/usr/bin/env bash
printf '{"status":"completed","tool_key":"huginn_visual_audit","summary":"Captured Huginn visual audit evidence for x-compose.","artifacts":{"target_url":"https://x.com/compose/post","final_url":"https://x.com/compose/post","title":"X","label":"x-compose","screenshot_path":"/tmp/x-compose.png","snapshot_excerpt":"What is happening?!","wait_ms":"2000","launch_mode":"--headed"}}'
`
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile(driver) error = %v", err)
	}
	if err := os.Chmod(scriptPath, 0o755); err != nil {
		t.Fatalf("Chmod(driver) error = %v", err)
	}
	t.Setenv("ODIN_HUGINN_VISUAL_DRIVER", scriptPath)

	var output bytes.Buffer
	if err := shell.HandleLine(ctx, "/tool run huginn_visual_audit target_url=https://x.com/compose/post label=x-compose headless=false", &output); err != nil {
		t.Fatalf("HandleLine(/tool run global visual audit) error = %v", err)
	}
	if !strings.Contains(output.String(), "artifact final_url=https://x.com/compose/post") {
		t.Fatalf("output = %q, want final_url artifact", output.String())
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/repl -run TestShellToolRunInvokesLiveVisualAuditToolInGlobalScope -count=1`

Expected: FAIL because `huginn_visual_audit` is not yet available in `global` scope.

**Step 3: Write minimal implementation**

```go
{
	Key:        "huginn_visual_audit",
	Title:      "Huginn Visual Audit",
	Summary:    "Captures a live Huginn browser snapshot and screenshot for a visual review target.",
	Scopes:     []string{"global", "project", "odin-core"},
	// ...
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/tools/catalog ./internal/cli/repl`

Expected: PASS

**Step 5: Commit**

```bash
git add internal/tools/catalog/builtin.go internal/cli/repl/shell_test.go
git commit -m "feat: allow global visual audit for social preflight"
```

### Task 2: Publish The Canonical Marcus Operations Runbook

**Domain Goal:** Put the first Marcus-live X-post sequence in the doc location the domain model already chose: `docs/operations/`.

**Domain Rules Enforced:**
- Durable semantics stay in `docs/contracts/`
- Step-by-step operator procedure lives in `docs/operations/`
- No new wrapper command surface

**Why this matters:**
- The social contract should not silently absorb operator procedure. The runbook is the executable human-facing artifact.

**Files:**
- Create: `docs/operations/marcus-live-x-post-runbook.md`
- Modify: `docs/contracts/marcus-social-copilot.md`

**Step 1: Write the failing test**

```bash
test -f docs/operations/marcus-live-x-post-runbook.md
rg -n "marcus-live-x-post-runbook" docs/contracts/marcus-social-copilot.md
```

**Step 2: Run test to verify it fails**

Run:

```bash
test -f docs/operations/marcus-live-x-post-runbook.md
rg -n "marcus-live-x-post-runbook" docs/contracts/marcus-social-copilot.md
```

Expected: FAIL because the dedicated runbook file and contract pointer are missing.

**Step 3: Write minimal implementation**

```md
# Marcus Live X Post Runbook

Use this runbook for the first Marcus-live X post loop only.

1. bootstrap the shell from `~/.config/odin/odin.env`
2. run `./bin/odin healthcheck` and `./bin/odin doctor --json`
3. enter `./bin/odin`, reset to `/scope global`, validate/select workflow and skill
4. run headed `/tool run huginn_visual_audit target_url=https://x.com/compose/post ...`
5. draft, review, approve, publish, prove published outcome, capture visible evidence, prove evidence row
6. clear `/workflow`, `/skill`, `/scope global`, then `exit`
```

Add one contract pointer:

```md
For the first Marcus-live X-post operator path, use [docs/operations/marcus-live-x-post-runbook.md](../operations/marcus-live-x-post-runbook.md).
```

**Step 4: Run test to verify it passes**

Run:

```bash
test -f docs/operations/marcus-live-x-post-runbook.md
rg -n "marcus-live-x-post-runbook" docs/contracts/marcus-social-copilot.md
```

Expected: PASS

**Step 5: Commit**

```bash
git add docs/operations/marcus-live-x-post-runbook.md docs/contracts/marcus-social-copilot.md
git commit -m "docs: add Marcus live X post runbook"
```

### Task 3: Align The Live Driver Contract With The Social Driver Surface

**Domain Goal:** Keep the driver contract honest about the env vars the social live loop actually uses.

**Domain Rules Enforced:**
- Reuse existing driver env vars and scripts
- Do not invent a new launcher or parallel config surface
- Repo contracts must not drift from the live operator path

**Why this matters:**
- If the live-driver contract omits the X publish driver from its env var list, the repo tells two different stories about the same operator surface.

**Files:**
- Modify: `docs/contracts/live-driver-tools.md`

**Step 1: Write the failing test**

```bash
sed -n '11,17p' docs/contracts/live-driver-tools.md | rg 'ODIN_HUGINN_X_PUBLISH_DRIVER'
```

**Step 2: Run test to verify it fails**

Run: `sed -n '11,17p' docs/contracts/live-driver-tools.md | rg 'ODIN_HUGINN_X_PUBLISH_DRIVER'`

Expected: FAIL because the env-var list omits `ODIN_HUGINN_X_PUBLISH_DRIVER`.

**Step 3: Write minimal implementation**

```md
## Environment variables

- `ODIN_GOOGLE_CALENDAR_DRIVER`
- `ODIN_HUGINN_DRIVER`
- `ODIN_HUGINN_VISUAL_DRIVER`
- `ODIN_HUGINN_X_POST_DRIVER`
- `ODIN_HUGINN_X_PUBLISH_DRIVER`
```

Keep the rest of the document on the existing driver surface; do not invent new social drivers.

**Step 4: Run test to verify it passes**

Run: `sed -n '11,18p' docs/contracts/live-driver-tools.md | rg 'ODIN_HUGINN_X_PUBLISH_DRIVER'`

Expected: PASS

**Step 5: Commit**

```bash
git add docs/contracts/live-driver-tools.md
git commit -m "docs: align live driver contract with social publish driver"
```

### Task 4: Prove The Safe Real Odin Path And Keep Marcus-Live Separate

**Domain Goal:** Prove the real repo-local binary path on safe non-posting commands, then stop at the operator-attended live X boundary.

**Domain Rules Enforced:**
- Use the repo-local binary from `/home/orchestrator/odin-os`
- Keep machine-local operator config in `~/.config/odin/odin.env`
- Do not claim Marcus-live readiness from fixture-backed proof alone
- Stop before live compose confirmation or live publish unless an operator is present

**Why this matters:**
- This task is the difference between “code seems right” and “the real operator surface is usable on this machine”.

**Files:**
- Create local only: `~/.config/odin/odin.env` (not committed)
- Modify only if real output drifts from docs: `docs/operations/marcus-live-x-post-runbook.md`
- Verify: `./bin/odin`

**Step 1: Write the failing test**

```bash
test -f ~/.config/odin/odin.env
```

**Step 2: Run test to verify it fails**

Run: `test -f ~/.config/odin/odin.env`

Expected: FAIL on a machine that has not been bootstrapped for Marcus live social ops yet.

**Step 3: Write minimal implementation**

Create the local env file in plain assignment format:

```bash
mkdir -p ~/.config/odin ~/.local/share/odin/marcus-social-live
cat > ~/.config/odin/odin.env <<'EOF'
ODIN_ROOT=/home/orchestrator/.local/share/odin/marcus-social-live
ODIN_HUGINN_VISUAL_DRIVER=/home/orchestrator/odin-os/scripts/drivers/huginn-visual-audit.sh
ODIN_HUGINN_X_PUBLISH_DRIVER=/home/orchestrator/odin-os/scripts/drivers/huginn-x-post-publish.sh
ODIN_HUGINN_X_POST_DRIVER=/home/orchestrator/odin-os/scripts/drivers/huginn-x-post-evidence.sh
EOF
```

**Step 4: Run test to verify it passes**

Run:

```bash
set -a
. ~/.config/odin/odin.env
set +a
test -x "$ODIN_HUGINN_VISUAL_DRIVER"
test -x "$ODIN_HUGINN_X_PUBLISH_DRIVER"
test -x "$ODIN_HUGINN_X_POST_DRIVER"
cd /home/orchestrator/odin-os
go build -o ./bin/odin ./cmd/odin
./bin/odin healthcheck
./bin/odin doctor --json
printf '/scope global\n/tool list\nexit\n' | ./bin/odin
```

Expected:
- all three driver `test -x` checks pass
- `./bin/odin healthcheck` prints `ready`
- `./bin/odin doctor --json` reports healthy or honest degraded state
- global `/tool list` includes `huginn_visual_audit`

**Step 5: Operator-attended boundary check**

Only with an operator present:

```bash
cd /home/orchestrator/odin-os
set -a
. ~/.config/odin/odin.env
set +a
printf '/scope global\n/tool run huginn_visual_audit target_url=https://x.com/compose/post label=x-compose-preflight headless=false\nexit\n' | ./bin/odin
```

Expected:
- Odin returns tool output normally
- Marcus manually confirms the screenshot and `final_url`

If the compose page is not visibly correct, stop and treat the session as not Marcus-live-ready yet.

**Step 6: Commit**

If no repo files changed during proof, do not create a no-op commit.

If the real output forced a doc correction:

```bash
git add docs/operations/marcus-live-x-post-runbook.md
git commit -m "docs: align Marcus runbook with verified shell output"
```

## Invariant Coverage Map

- Global social operator surface stays coherent:
  - `internal/cli/repl/shell_test.go::TestShellToolRunInvokesLiveVisualAuditToolInGlobalScope`
  - real `./bin/odin` global `/tool list` proof
- Durable-vs-operations doc split stays explicit:
  - `docs/contracts/marcus-social-copilot.md`
  - `docs/operations/marcus-live-x-post-runbook.md`
- Driver contract matches the live social surface:
  - `docs/contracts/live-driver-tools.md`
- Repo proof stays distinct from Marcus-live operator proof:
  - `./bin/odin healthcheck`
  - `./bin/odin doctor --json`
  - operator-attended compose preflight only as the final manual gate

## Blockers To Surface, Not Hide

- `~/.config/odin/odin.env` missing or misconfigured
- any of the three social driver paths not executable
- `./bin/odin doctor --json` degraded in a way the operator has not consciously accepted
- global `/tool list` missing `huginn_visual_audit`
- headed compose preflight not showing Marcus’s logged-in X compose page

## Review Checklist

- domain naming matches `CONTEXT.md`
- invariant coverage exists and is named above
- ADR 0001 authority rules are honored
- ADR 0002 migration rules are honored
- boundary crossings are explicit and justified
- reused repo structures are named
- remaining Marcus-live browser/session proof is listed as an operator-attended gate, not hidden in automation
