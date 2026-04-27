# Social Retrospective Prompt Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make the Marcus weekly retrospective repeatable by automatically assembling the last 7 days of high-signal social memory into the existing workflow + analytics advisor prompt path.

**Architecture:** Reuse the current shell prompt-enrichment path, workflow-scoped knowledge memory, and existing skill/workflow selection. Add narrow retrospective context assembly only when `marcus-social-growth-workflow` and `marcus-social-analytics-advisor` are both selected, then prepend a bounded social-memory context block and platform voice framing ahead of the task request.

**Tech Stack:** Go 1.25, REPL shell under `internal/cli/repl`, existing knowledge-memory service and SQLite-backed memory summaries, markdown docs under `docs/contracts` and `docs/plans`.

---

## Preconditions

- Use [2026-04-18-social-retrospective-prompt-design.md](/home/orchestrator/odin-os/docs/plans/2026-04-18-social-retrospective-prompt-design.md) as the design authority.
- Do not add a new retrospective command.
- Do not add a migration or new memory table.
- Keep the first slice limited to the Marcus workflow + analytics advisor combination.
- Prove the finished behavior through the real `odin` binary in `odin-os`.

### Task 1: Lock the retrospective prompt contract with failing shell tests

**Files:**
- Modify: `internal/cli/repl/shell_test.go`

**Step 1: Write the failing tests**

Add tests like:

```go
func TestShellAnalyticsPromptIncludesRecentSocialRetrospectiveContext(t *testing.T) {}
func TestShellAnalyticsPromptSkipsOlderSocialMemory(t *testing.T) {}
func TestShellAnalyticsPromptDoesNotIncludeSocialDrafts(t *testing.T) {}
func TestShellNonAnalyticsPromptDoesNotAutoInjectRetrospectiveContext(t *testing.T) {}
```

Core assertions:

- when workflow=`marcus-social-growth-workflow` and skill=`marcus-social-analytics-advisor`, the prompt includes:
  - retrospective window: last 7 days
  - recent approved and rejected outcomes
  - recent learnings and research
  - X voice framing
  - LinkedIn voice framing
- old memory is excluded
- `social_draft` is excluded
- the enrichment does not appear for unrelated skills

**Step 2: Run the tests to verify they fail**

Run: `go test ./internal/cli/repl -run 'TestShell.*RetrospectiveContext|TestShell.*AnalyticsPrompt' -count=1`

Expected: FAIL because the prompt path does not yet load or inject retrospective memory.

**Step 3: Write the minimal implementation contract updates**

Do not change production behavior yet beyond any test fixtures needed to seed social memory with timestamps.

**Step 4: Run the tests again to verify they still fail on runtime behavior**

Run: `go test ./internal/cli/repl -run 'TestShell.*RetrospectiveContext|TestShell.*AnalyticsPrompt' -count=1`

Expected: FAIL because context assembly is still missing.

### Task 2: Add bounded retrospective memory assembly to the shell prompt path

**Files:**
- Modify: `internal/cli/repl/shell.go`

**Step 1: Write any additional failing tests needed for helper behavior**

If helpful, add focused tests for helper output ordering and type filtering.

**Step 2: Run the tests to verify they fail**

Run: `go test ./internal/cli/repl -run 'TestShell.*RetrospectiveContext|TestShell.*AnalyticsPrompt' -count=1`

Expected: FAIL.

**Step 3: Write the minimal implementation**

Refactor `executionRequestForPrompt(...)` to accept `context.Context` so it can load memory.

Add narrow helpers in `internal/cli/repl/shell.go`, for example:

```go
func (shell *Shell) retrospectivePromptContext(ctx context.Context, workflow registry.Item, skill registry.Item) (string, error)
func (shell *Shell) shouldInjectSocialRetrospectiveContext(workflow registry.Item, hasWorkflow bool, skill registry.Item, hasSkill bool) bool
func (shell *Shell) recentSocialMemories(ctx context.Context, since time.Time) ([]sqlite.MemorySummary, error)
func summarizeRetrospectiveMemories(now time.Time, summaries []sqlite.MemorySummary) string
```

Rules:

- activate only for:
  - workflow key `marcus-social-growth-workflow`
  - skill key `marcus-social-analytics-advisor`
- use `shell.memoryScope(ctx)` for scope resolution
- load `social_outcome`, `social_learning`, and `social_research` separately through the existing knowledge-memory service
- keep only entries within `now - 7 days`
- keep the newest entries first
- cap each type to the bounded counts from the design
- split outcomes into approved vs rejected by `details_json.fields.result`
- inject explicit platform framing:
  - `X`: inner thoughts, perspective, conviction, tension, concise expression
  - `LinkedIn`: professional framing, practical lessons, peer-level clarity
- if no recent memory exists, emit a short “no recent retrospective memory found” note instead of failing

Compose the final prompt by inserting the retrospective block before `Task Request:`.

**Step 4: Run the tests to verify they pass**

Run: `go test ./internal/cli/repl -run 'TestShell.*RetrospectiveContext|TestShell.*AnalyticsPrompt' -count=1`

Expected: PASS.

### Task 3: Update the Marcus social contract to show the live retrospective path

**Files:**
- Modify: `docs/contracts/marcus-social-copilot.md`

**Step 1: Update the roadmap and examples**

Mark repeatable weekly retrospective prompts as live if the implementation verifies successfully.

Update CLI examples so they show:

```text
/workflow use marcus-social-growth-workflow
/skill use marcus-social-analytics-advisor
Summarize the last week of social outcomes and tell me what X should sound like next week versus LinkedIn.
```

Also reflect the voice split explicitly in the narrative where helpful:

- X = inner thoughts and perspective
- LinkedIn = more professional framing and practical lessons

### Task 4: Prove the feature through the real `odin` command

**Files:**
- Verify only

**Step 1: Run targeted tests**

Run:

```bash
go test ./internal/cli/repl ./internal/cli/commands ./internal/memory/knowledge -count=1
```

Expected: PASS.

**Step 2: Build the real binary**

Run:

```bash
make build
```

Expected: `go build -o bin/odin ./cmd/odin`

**Step 3: Run real `odin` verification with recent social history**

Run:

```bash
tmp_input=$(mktemp -p /tmp odin-social-retrospective-input-XXXXXXXX)
cat > "$tmp_input" <<'EOF'
/workflow use marcus-social-growth-workflow
/memory remember social_outcome result=approved channel=linkedin content_kind=post -- LinkedIn post approved for queue
/memory remember social_outcome result=rejected channel=x content_kind=reply reason=too-defensive -- X reply rejected for tone
/memory remember social_learning channel=x -- Stronger inner-thought framing worked better than generic advice.
/memory remember social_research channel=linkedin -- Airline training professionalism performed better than hustle language.
/skill use marcus-social-analytics-advisor
Give me this week's retrospective and tell me how next week's X voice should differ from LinkedIn.
/exit
EOF
ODIN_ROOT=$(mktemp -d -p /tmp odin-social-retrospective-root-XXXXXXXX)
env ODIN_ROOT="$ODIN_ROOT" ./bin/odin < "$tmp_input"
```

Expected:

- the live prompt/answer path includes retrospective context
- approved and rejected outcomes are represented
- X and LinkedIn voice framing is present

**Step 4: Run real `odin` verification with no recent social memory**

Run:

```bash
tmp_input=$(mktemp -p /tmp odin-social-retrospective-empty-input-XXXXXXXX)
cat > "$tmp_input" <<'EOF'
/workflow use marcus-social-growth-workflow
/skill use marcus-social-analytics-advisor
Give me this week's retrospective.
/exit
EOF
ODIN_ROOT=$(mktemp -d -p /tmp odin-social-retrospective-empty-root-XXXXXXXX)
env ODIN_ROOT="$ODIN_ROOT" ./bin/odin < "$tmp_input"
```

Expected:

- the shell still responds
- the prompt path notes that no recent retrospective memory was found

**Step 5: Final verification review**

Before claiming completion, confirm:

- tests are green
- build succeeds
- real `odin` retrospective path works with recent memory
- real `odin` retrospective path degrades cleanly with no recent memory
- no new command or memory schema was introduced
