# Odin Doctor Operator Report Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Extend `odin doctor` into a native, operator-grade Odin self-health surface with markdown reporting while preserving the existing machine-readable JSON contract.

**Architecture:** Keep `internal/runtime/health.Service.Doctor()` as the raw evidence collector that powers `/healthz`, `/readyz`, `odin doctor --json`, and REPL `/doctor json`. Add a separate operator report builder and markdown renderer in the same package, then wire `odin doctor --format markdown` and REPL `/doctor report` to that richer explanation layer without creating a parallel `ops` command family.

**Tech Stack:** Go, standard library, existing SQLite-backed runtime health service, repo-owned `cmd/odin` entrypoint, Go test suite.

---

## Context You Need Before Touching Code

- Work from the isolated worktree, not the dirty checkout: `/home/orchestrator/.worktrees/odin-os-ops-health-plan`
- The clean branch already has:
  - CLI `doctor` in `internal/app/lifecycle/run.go`
  - REPL `/doctor` in `internal/cli/repl/shell.go`
  - raw health checks in `internal/runtime/health/service.go`
  - health endpoint coverage in `internal/api/http/operational_test.go`
- The clean branch does **not** have the uncommitted `internal/runtime/conversation` package visible in the dirty checkout. Do not plan work against files that only exist in `/home/orchestrator/odin-os` but not in the isolated worktree.
- Preserve the current `doctor --json` shape at the top level: existing tests and any external callers expect `status`, `generated_at`, and `checks`.
- Do not change `/healthz` or `/readyz` to emit operator markdown or expanded prose.

### Task 1: Build the operator report model from raw checks

**Files:**
- Create: `internal/runtime/health/operator_report.go`
- Create: `internal/runtime/health/operator_report_test.go`
- Modify: `internal/runtime/health/doctor_test.go`
- Reuse: `internal/runtime/health/service.go`

**Step 1: Write the failing tests**

Add tests that define the operator report contract built from a raw `Report`:

```go
func TestBuildOperatorReportRanksFailuresBeforeDegradedFindings(t *testing.T) {
	raw := Report{
		Status: StatusFailed,
		Checks: []Check{
			{Name: "database", Status: StatusFailed, Summary: "database connectivity failed"},
			{Name: "queue", Status: StatusDegraded, Summary: "queue pressure is above threshold"},
		},
	}

	got := BuildOperatorReport(raw)

	if len(got.Findings) < 2 {
		t.Fatalf("Findings len = %d, want at least 2", len(got.Findings))
	}
	if got.Findings[0].Area != "database" || got.Findings[0].Severity != SeverityCritical {
		t.Fatalf("first finding = %+v, want critical database finding", got.Findings[0])
	}
}
```

Add a second test for missing telemetry:

```go
func TestBuildOperatorReportFlagsMissingTelemetry(t *testing.T) {
	raw := Report{
		Status: StatusDegraded,
		Checks: []Check{
			{Name: "executor", Status: StatusDegraded, Summary: "no executor health samples recorded"},
		},
	}

	got := BuildOperatorReport(raw)

	if len(got.MissingTelemetry) == 0 {
		t.Fatalf("MissingTelemetry = 0, want executor gap")
	}
}
```

Update `doctor_test.go` with a serialization guard that confirms the raw `Report` stays machine-parseable and unchanged at the top level.

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/health -run 'TestBuildOperatorReport|TestDoctorReportIsMachineParseable' -count=1`

Expected: FAIL with missing `BuildOperatorReport`, unknown operator report types, or assertion failures.

**Step 3: Write the minimal implementation**

Create the operator report types and builder in `operator_report.go`:

```go
type Severity string

const (
	SeverityCritical Severity = "critical"
	SeverityHigh     Severity = "high"
	SeverityMedium   Severity = "medium"
)

type OperatorReport struct {
	CurrentHealth    CurrentHealthSnapshot `json:"current_health"`
	Findings         []Finding             `json:"findings"`
	RootCauses       []RootCause           `json:"root_causes"`
	Recommendations  RecommendationGroups  `json:"recommendations"`
	MissingTelemetry []string              `json:"missing_telemetry"`
	FinalVerdict     FinalVerdict          `json:"final_verdict"`
}
```

Implement `BuildOperatorReport(raw Report) OperatorReport` with deterministic rules:

- map raw `failed` checks to critical/high findings
- map raw `degraded` checks to high/medium findings
- treat `no ... recorded`, `missing`, and `stale` summaries as telemetry-confidence problems where appropriate
- populate grouped recommendations without external lookups
- keep the mapping table local and explicit, not heuristic and sprawling

Do **not** change `Service.Doctor()` yet. This task only builds the explanation layer from the existing raw report.

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/health -run 'TestBuildOperatorReport|TestDoctorReportIsMachineParseable' -count=1`

Expected: PASS

**Step 5: Commit**

```bash
git add internal/runtime/health/operator_report.go \
  internal/runtime/health/operator_report_test.go \
  internal/runtime/health/doctor_test.go
git commit -m "feat(doctor): add operator report model"
```

### Task 2: Render the operator report as markdown

**Files:**
- Create: `internal/runtime/health/markdown.go`
- Create: `internal/runtime/health/markdown_test.go`
- Reuse: `internal/runtime/health/operator_report.go`

**Step 1: Write the failing tests**

Add markdown rendering tests that assert the required operator sections appear and stay ordered:

```go
func TestRenderMarkdownReportIncludesOperatorSections(t *testing.T) {
	report := OperatorReport{
		Findings: []Finding{{Area: "database", Severity: SeverityCritical}},
		MissingTelemetry: []string{"executor freshness samples"},
	}

	output := RenderMarkdownReport(report)

	for _, want := range []string{
		"## Current Health Snapshot",
		"## Key Findings",
		"## Likely Root Causes",
		"## Upgrade and Improvement Recommendations",
		"## Missing Telemetry",
		"## Final Verdict",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q\n%s", want, output)
		}
	}
}
```

Add a second test that checks findings are rendered before recommendations and that the findings table contains the expected columns.

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/health -run 'TestRenderMarkdownReport' -count=1`

Expected: FAIL with missing renderer or missing required sections.

**Step 3: Write the minimal implementation**

Implement a string-builder-based renderer:

```go
func RenderMarkdownReport(report OperatorReport) string {
	var builder strings.Builder
	builder.WriteString("## Current Health Snapshot\n")
	// ...
	builder.WriteString("## Key Findings\n")
	builder.WriteString("| Area | Severity | Observation | Why It Matters | Confidence |\n")
	// ...
	return builder.String()
}
```

Rules:

- hard-code the required section order from the approved design
- render empty sections explicitly with `None` or `No major issues detected` where needed
- keep wording deterministic; avoid free-form LLM-style narrative
- use the same terminology as the approved report format: `Immediate`, `Near-Term`, `Strategic`

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/health -run 'TestRenderMarkdownReport' -count=1`

Expected: PASS

**Step 5: Commit**

```bash
git add internal/runtime/health/markdown.go internal/runtime/health/markdown_test.go
git commit -m "feat(doctor): render markdown operator reports"
```

### Task 3: Wire `odin doctor --format markdown` without breaking `--json`

**Files:**
- Modify: `internal/app/lifecycle/run.go`
- Modify: `internal/app/lifecycle/serve_test.go`
- Reuse: `internal/runtime/health/service.go`
- Reuse: `internal/runtime/health/operator_report.go`
- Reuse: `internal/runtime/health/markdown.go`

**Step 1: Write the failing tests**

Extend `internal/app/lifecycle/serve_test.go` with a markdown-mode CLI test:

```go
func TestRunDoctorMarkdownWritesOperatorReport(t *testing.T) {
	root := createRuntimeRoot(t)

	var stdout bytes.Buffer
	if err := Run(context.Background(), root, []string{"doctor", "--format", "markdown"}, strings.NewReader(""), &stdout); err != nil {
		t.Fatalf("Run(doctor --format markdown) error = %v", err)
	}

	if !strings.Contains(stdout.String(), "## Current Health Snapshot") {
		t.Fatalf("doctor markdown output = %q, want report heading", stdout.String())
	}
}
```

Add a guard test for unsupported formats:

```go
func TestRunDoctorRejectsUnknownFormat(t *testing.T) {
	err := Run(context.Background(), root, []string{"doctor", "--format", "yaml"}, strings.NewReader(""), &stdout)
	if err == nil {
		t.Fatalf("Run(doctor --format yaml) error = nil, want rejection")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/app/lifecycle -run 'TestRunDoctor(JSONWritesStructuredReport|MarkdownWritesOperatorReport|RejectsUnknownFormat)' -count=1`

Expected: FAIL because `runDoctor` only understands `--json`.

**Step 3: Write the minimal implementation**

Refactor `runDoctor` argument parsing so it supports:

- default text summary: current compact output
- `--json`: existing raw report JSON
- `--format markdown`: operator markdown
- optional alias `--report`: same as markdown if the parsing stays simple

Suggested shape:

```go
type doctorOutputMode string

const (
	doctorOutputSummary  doctorOutputMode = "summary"
	doctorOutputJSON     doctorOutputMode = "json"
	doctorOutputMarkdown doctorOutputMode = "markdown"
)
```

Implementation rules:

- build the raw report once
- only build the operator report when markdown output is requested
- reject unknown flags and formats with a clear error
- leave `/healthz` and `/readyz` untouched

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/app/lifecycle -run 'TestRunDoctor(JSONWritesStructuredReport|MarkdownWritesOperatorReport|RejectsUnknownFormat)' -count=1`

Expected: PASS

**Step 5: Commit**

```bash
git add internal/app/lifecycle/run.go internal/app/lifecycle/serve_test.go
git commit -m "feat(doctor): add markdown output mode"
```

### Task 4: Add REPL `/doctor report` and keep plain `/doctor` compact

**Files:**
- Modify: `internal/cli/repl/shell.go`
- Modify: `internal/cli/repl/shell_test.go`
- Modify: `internal/cli/commands/commands_test.go`
- Reuse: `internal/runtime/health/markdown.go`

**Step 1: Write the failing tests**

Add a REPL report test next to the existing `/doctor json` test:

```go
func TestShellDoctorReportWritesMarkdownSummary(t *testing.T) {
	env := newTestEnvironment(t)
	seedHealthyDoctorState(t, env)

	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	if err := shell.HandleLine(context.Background(), "/doctor report", &output); err != nil {
		t.Fatalf("HandleLine(/doctor report) error = %v", err)
	}

	if !strings.Contains(output.String(), "## Current Health Snapshot") {
		t.Fatalf("output = %q, want markdown doctor report", output.String())
	}
}
```

Add a small `commands_test.go` assertion that slash parsing preserves the second token:

```go
func TestParseSlashCommandWithSubargument(t *testing.T) {
	command, ok := Parse("/doctor report")
	if !ok || command.Name != "doctor" || len(command.Args) != 1 || command.Args[0] != "report" {
		t.Fatalf("Parse(/doctor report) = %#v, %#v", command, ok)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/commands ./internal/cli/repl -run 'TestParseSlashCommandWithSubargument|TestShellDoctorReportWritesMarkdownSummary|TestShellDoctorJSONWritesMachineReadableReport' -count=1`

Expected: FAIL because `/doctor report` is not recognized yet.

**Step 3: Write the minimal implementation**

Extend `handleDoctor` so it supports:

- no args: current compact summary
- `json`: existing raw JSON
- `report`: markdown operator report

Implementation pattern:

```go
switch {
case len(args) > 0 && strings.EqualFold(args[0], "json"):
	// existing path
case len(args) > 0 && strings.EqualFold(args[0], "report"):
	_, err = fmt.Fprint(output, healthsvc.RenderMarkdownReport(healthsvc.BuildOperatorReport(report)))
default:
	// current compact summary
}
```

Keep ask-mode routing unchanged. On this clean branch there is no separate conversation service, so health-related ask intent already lands on `/doctor`.

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/commands ./internal/cli/repl -run 'TestParseSlashCommandWithSubargument|TestShellDoctorReportWritesMarkdownSummary|TestShellDoctorJSONWritesMachineReadableReport' -count=1`

Expected: PASS

**Step 5: Commit**

```bash
git add internal/cli/commands/commands_test.go internal/cli/repl/shell.go internal/cli/repl/shell_test.go
git commit -m "feat(repl): add doctor report output"
```

### Task 5: Add end-to-end verification on the real Odin command path

**Files:**
- Modify: `tests/integration/alpha_acceptance_test.go`
- Reuse: `cmd/odin/main.go`
- Reuse: `internal/app/lifecycle/run.go`

**Step 1: Write the failing test**

Extend the existing `observability and doctor surfaces are useful` acceptance subtest so it exercises both doctor modes through the built binary:

```go
output, err := runOdinCommand(t, repoRoot, odinBinary, runtimeRoot, nil, "", "doctor", "--format", "markdown")
if err != nil {
	t.Fatalf("runOdinCommand(doctor --format markdown) error = %v\n%s", err, output)
}
if !strings.Contains(output, "## Final Verdict") {
	t.Fatalf("doctor markdown output = %q, want final verdict", output)
}
```

Keep the existing `doctor --json` acceptance assertion in place.

**Step 2: Run the targeted acceptance test to verify it fails**

Run: `go test ./tests/integration -run 'TestAlphaAcceptance/observability_and_doctor_surfaces_are_useful' -count=1`

Expected: FAIL because the binary does not yet support markdown doctor output.

**Step 3: Run focused package tests after the code from prior tasks is in place**

Run:

```bash
go test ./internal/runtime/health ./internal/app/lifecycle ./internal/cli/commands ./internal/cli/repl -count=1
go test ./tests/integration -run 'TestAlphaAcceptance/observability_and_doctor_surfaces_are_useful' -count=1
```

Expected: PASS

**Step 4: Run real command-path verification**

From the worktree root, run:

```bash
go run ./cmd/odin doctor --json
go run ./cmd/odin doctor --format markdown
```

Verify:

- `doctor --json` prints valid JSON with `status`
- `doctor --format markdown` prints the operator report sections, including `Current Health Snapshot`, `Key Findings`, `Missing Telemetry`, and `Final Verdict`

If local runtime setup is required, document the exact env vars or config used when recording results.

**Step 5: Commit**

```bash
git add tests/integration/alpha_acceptance_test.go
git commit -m "test(doctor): cover markdown doctor output end to end"
```

### Task 6: Run broader verification and record any baseline failures honestly

**Files:**
- Modify if needed: `docs/plans/2026-04-17-odin-doctor-ops-report.md`

**Step 1: Run broader verification**

Run:

```bash
go test ./... -count=1
```

If the full suite is too expensive or known-flaky, at minimum run:

```bash
go test ./internal/... ./tests/integration -count=1
```

**Step 2: Capture exact pass/fail outcomes**

Record:

- focused package test results
- acceptance test results
- real `go run ./cmd/odin doctor ...` results
- any unrelated baseline failures that remain

Do not claim success if only unit tests pass and the real `cmd/odin` path was not exercised.

**Step 3: Final commit if verification notes changed**

```bash
git add docs/plans/2026-04-17-odin-doctor-ops-report.md
git commit -m "docs(plan): record doctor report verification outcomes"
```

## Final Verification Checklist

- `go test ./internal/runtime/health -count=1`
- `go test ./internal/app/lifecycle -count=1`
- `go test ./internal/cli/commands ./internal/cli/repl -count=1`
- `go test ./tests/integration -run 'TestAlphaAcceptance/observability_and_doctor_surfaces_are_useful' -count=1`
- `go run ./cmd/odin doctor --json`
- `go run ./cmd/odin doctor --format markdown`

## Notes for the Implementer

- Preserve DRY: raw health collection stays in `Service.Doctor()`, explanation lives in a builder, formatting lives in a renderer.
- Preserve YAGNI: do not add host probing, SSH, homelab checks, or Hetzner checks in this phase.
- Preserve backward compatibility: `doctor --json`, `/doctor json`, `/healthz`, and `/readyz` should remain machine-oriented and structurally familiar.
- Prefer explicit rule tables over clever heuristics when mapping checks to findings and recommendations.
