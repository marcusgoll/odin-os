# Phase 01 Repo Scaffold Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Create the clean Odin OS repository scaffold, initialize a minimal Go module, and add basic local and CI verification entrypoints.

**Architecture:** The scaffold keeps Phase 01 honest by creating the documented directory layout, a minimal `cmd/odin` entrypoint backed by one tiny lifecycle package, and non-speculative build tooling. The only runtime behavior is a placeholder startup message that proves the command builds and runs without pretending later phases are implemented.

**Tech Stack:** Go 1.22, GNU Make, Git, GitHub Actions

---

### Task 1: Initialize repository governance and scaffold directories

**Files:**
- Create: `.gitignore`
- Create: `.github/workflows/ci.yml`
- Create: `.gitkeep` files under scaffold directories that would otherwise be empty
- Modify: `README.md`

**Step 1: Create the directory layout**

Run:

```bash
mkdir -p cmd/odin \
  internal/app/{bootstrap,lifecycle,config} \
  internal/cli/{repl,commands,render,tui,scope} \
  internal/api/{http,websocket} \
  internal/core/{intake,router,context,approvals,policy,scheduler,orchestration,projects} \
  internal/runtime/{jobs,runs,events,projections,health,recovery,uncertainty,checkpoints} \
  internal/registry/{loader,parser,validator,compiler,watcher} \
  internal/learning/{evaluator,proposals,promotion,replay} \
  internal/memory/{users,projects,runs,knowledge} \
  internal/workers/{planner,builder,reviewer,qa,research} \
  internal/executors/{contract,router,claude_code,codex,gemini_cli,openai_api,anthropic_api,google_api,xai_api,openrouter_api} \
  internal/tools/{broker,catalog,invocation,budgets} \
  internal/vcs/{git,worktrees,branches,leases} \
  internal/adapters/{github,shell,filesystem,web,gmail,calendar} \
  internal/store/sqlite \
  internal/telemetry/{logs,metrics,traces,audit} \
  registry/{agents,skills,workflows,commands} \
  prompts/{system,workers,templates} \
  memory/{users,projects} \
  config data runs/{logs,artifacts,summaries} \
  state/{cache,snapshots,compiled} \
  docs/{migration,operations} scripts/{migrate,dev,ci} tests/{unit,integration,replay}
```

**Step 2: Initialize git**

Run:

```bash
git init
```

Expected: repository initialized in `/home/orchestrator/odin-os/.git/`

**Step 3: Add ignore and retention files**

Write `.gitignore` and `.gitkeep` placeholders so the scaffold directories are tracked without promoting caches or runtime outputs to canonical sources.

**Step 4: Update status text**

Update `README.md` to note that Phase 01 adds the scaffold and minimal command skeleton.

### Task 2: Add the minimal buildable Go skeleton with TDD

**Files:**
- Create: `go.mod`
- Create: `cmd/odin/main.go`
- Create: `internal/app/lifecycle/run_test.go`
- Create: `internal/app/lifecycle/run.go`
- Create: `internal/*/doc.go` for top-level package placeholders where useful

**Step 1: Write the failing test**

Create `internal/app/lifecycle/run_test.go` that expects `Run(context.Context, io.Writer) error` to emit a short scaffold message and return `nil`.

**Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/app/lifecycle
```

Expected: FAIL because `Run` is undefined

**Step 3: Add minimal implementation**

Initialize the module as `odin-os`, implement `Run`, and wire `cmd/odin/main.go` to call it.

**Step 4: Run test to verify it passes**

Run:

```bash
go test ./internal/app/lifecycle
```

Expected: PASS

### Task 3: Add local runner commands and CI verification

**Files:**
- Create: `Makefile`

**Step 1: Add local commands**

Define `format`, `fmtcheck`, `lint`, `test`, and `build` targets using `gofmt`, `go vet`, `go test`, and `go build`.

**Step 2: Add CI**

Create a basic GitHub Actions workflow that runs:

```bash
make fmtcheck
make lint
make test
make build
```

**Step 3: Verify the full scaffold**

Run:

```bash
make format
make lint
make test
make build
```

Expected: all commands exit 0
