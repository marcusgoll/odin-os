package integration_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestWorkExecutionStateContract(t *testing.T) {
	repoRoot := projectRoot(t)
	odinBinary := buildOdinBinary(t, repoRoot)

	t.Run("contract document locks work and run state ownership", func(t *testing.T) {
		contractPath := filepath.Join(repoRoot, "docs", "contracts", "work-execution-state.md")
		content, err := os.ReadFile(contractPath)
		if err != nil {
			t.Fatalf("ReadFile(%s) error = %v", contractPath, err)
		}
		text := string(content)
		for _, want := range []string{
			"# Work Execution State Contract",
			"Canonical operator object: `Work Item`.",
			"Canonical execution object: `Run Attempt`.",
			"`drafted` is not a Work Item status.",
			"`approved` is not a Work Item status.",
			"`done` is never persisted as a primary Work Item status.",
			"| `queued` |",
			"| `running` |",
			"| `blocked` |",
			"| `failed` |",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("contract missing %q", want)
			}
		}
	})

	t.Run("work start and dispatch prove queued and running readback", func(t *testing.T) {
		runtimeRoot := t.TempDir()

		statusOutput, err := runOdinCommand(t, repoRoot, odinBinary, runtimeRoot, nil, "", "work", "status")
		if err != nil {
			t.Fatalf("runOdinCommand(work status) error = %v\n%s", err, statusOutput)
		}
		for _, want := range []string{"work_items=0", "active_run_attempts=0", "pending_approvals=0"} {
			if !strings.Contains(statusOutput, want) {
				t.Fatalf("initial work status = %q, want %s", statusOutput, want)
			}
		}

		startOutput, err := runOdinCommand(t, repoRoot, odinBinary, runtimeRoot, nil, "", "work", "start", "--project", "odin-core", "--title", "state contract queued proof", "--intent", "read_only")
		if err != nil {
			t.Fatalf("runOdinCommand(work start) error = %v\n%s", err, startOutput)
		}
		if !strings.Contains(startOutput, "status=queued") || !strings.Contains(startOutput, "intent=read_only") {
			t.Fatalf("work start output = %q, want queued read_only work item", startOutput)
		}

		jobsOutput, err := runOdinCommand(t, repoRoot, odinBinary, runtimeRoot, nil, "", "jobs", "--json")
		if err != nil {
			t.Fatalf("runOdinCommand(jobs --json) error = %v\n%s", err, jobsOutput)
		}
		var jobsView struct {
			Jobs []struct {
				TaskID                int64  `json:"task_id"`
				TaskKey               string `json:"task_key"`
				Status                string `json:"status"`
				ExecutionIntent       string `json:"execution_intent"`
				ExecutionIntentSource string `json:"execution_intent_source"`
				CurrentRunID          *int64 `json:"current_run_id"`
				CurrentRunStatus      string `json:"current_run_status"`
			} `json:"jobs"`
		}
		if err := json.Unmarshal([]byte(jobsOutput), &jobsView); err != nil {
			t.Fatalf("json.Unmarshal(jobs output) error = %v\n%s", err, jobsOutput)
		}
		if len(jobsView.Jobs) != 1 {
			t.Fatalf("jobs = %+v, want one queued work item", jobsView.Jobs)
		}
		queuedJob := jobsView.Jobs[0]
		if queuedJob.Status != "queued" || queuedJob.ExecutionIntent != "read_only" || queuedJob.ExecutionIntentSource != "operator" || queuedJob.CurrentRunID != nil {
			t.Fatalf("queued job = %+v, want queued read_only work item with no current run", queuedJob)
		}

		runsOutput, err := runOdinCommand(t, repoRoot, odinBinary, runtimeRoot, nil, "", "runs", "--json")
		if err != nil {
			t.Fatalf("runOdinCommand(runs --json) error = %v\n%s", err, runsOutput)
		}
		var runsView struct {
			Runs []struct {
				RunID    int64  `json:"run_id"`
				TaskID   int64  `json:"task_id"`
				TaskKey  string `json:"task_key"`
				Executor string `json:"executor"`
				Status   string `json:"status"`
			} `json:"runs"`
		}
		if err := json.Unmarshal([]byte(runsOutput), &runsView); err != nil {
			t.Fatalf("json.Unmarshal(initial runs output) error = %v\n%s", err, runsOutput)
		}
		if len(runsView.Runs) != 0 {
			t.Fatalf("runs = %+v, want no run attempts before dispatch", runsView.Runs)
		}

		dispatchOutput, err := runOdinCommand(t, repoRoot, odinBinary, runtimeRoot, nil, "", "work", "dispatch", "--task", strconv.FormatInt(queuedJob.TaskID, 10), "--json")
		if err != nil {
			t.Fatalf("runOdinCommand(work dispatch) error = %v\n%s", err, dispatchOutput)
		}
		var dispatchView struct {
			Dispatched bool   `json:"dispatched"`
			Reason     string `json:"reason"`
			Task       struct {
				ID           int64  `json:"id"`
				Status       string `json:"status"`
				CurrentRunID *int64 `json:"current_run_id"`
			} `json:"task"`
			Run *struct {
				ID       int64  `json:"id"`
				TaskID   int64  `json:"task_id"`
				Executor string `json:"executor"`
				Status   string `json:"status"`
			} `json:"run"`
		}
		if err := json.Unmarshal([]byte(dispatchOutput), &dispatchView); err != nil {
			t.Fatalf("json.Unmarshal(dispatch output) error = %v\n%s", err, dispatchOutput)
		}
		if !dispatchView.Dispatched || dispatchView.Reason != "dispatched" || dispatchView.Task.Status != "running" || dispatchView.Run == nil || dispatchView.Run.Status != "running" || dispatchView.Task.CurrentRunID == nil {
			t.Fatalf("dispatch = %+v, want running task and run attempt", dispatchView)
		}
		if *dispatchView.Task.CurrentRunID != dispatchView.Run.ID || dispatchView.Run.TaskID != queuedJob.TaskID {
			t.Fatalf("dispatch linkage = %+v, want task.current_run_id linked to run", dispatchView)
		}

		jobsOutput, err = runOdinCommand(t, repoRoot, odinBinary, runtimeRoot, nil, "", "jobs", "--json")
		if err != nil {
			t.Fatalf("runOdinCommand(jobs --json after dispatch) error = %v\n%s", err, jobsOutput)
		}
		jobsView = struct {
			Jobs []struct {
				TaskID                int64  `json:"task_id"`
				TaskKey               string `json:"task_key"`
				Status                string `json:"status"`
				ExecutionIntent       string `json:"execution_intent"`
				ExecutionIntentSource string `json:"execution_intent_source"`
				CurrentRunID          *int64 `json:"current_run_id"`
				CurrentRunStatus      string `json:"current_run_status"`
			} `json:"jobs"`
		}{}
		if err := json.Unmarshal([]byte(jobsOutput), &jobsView); err != nil {
			t.Fatalf("json.Unmarshal(jobs after dispatch) error = %v\n%s", err, jobsOutput)
		}
		if len(jobsView.Jobs) != 1 || jobsView.Jobs[0].Status != "running" || jobsView.Jobs[0].CurrentRunID == nil || *jobsView.Jobs[0].CurrentRunID != dispatchView.Run.ID || jobsView.Jobs[0].CurrentRunStatus != "running" {
			t.Fatalf("jobs after dispatch = %+v, want running work item linked to run", jobsView.Jobs)
		}

		runsOutput, err = runOdinCommand(t, repoRoot, odinBinary, runtimeRoot, nil, "", "runs", "--json")
		if err != nil {
			t.Fatalf("runOdinCommand(runs --json after dispatch) error = %v\n%s", err, runsOutput)
		}
		runsView = struct {
			Runs []struct {
				RunID    int64  `json:"run_id"`
				TaskID   int64  `json:"task_id"`
				TaskKey  string `json:"task_key"`
				Executor string `json:"executor"`
				Status   string `json:"status"`
			} `json:"runs"`
		}{}
		if err := json.Unmarshal([]byte(runsOutput), &runsView); err != nil {
			t.Fatalf("json.Unmarshal(runs after dispatch) error = %v\n%s", err, runsOutput)
		}
		if len(runsView.Runs) != 1 || runsView.Runs[0].RunID != dispatchView.Run.ID || runsView.Runs[0].TaskID != queuedJob.TaskID || runsView.Runs[0].Status != "running" {
			t.Fatalf("runs after dispatch = %+v, want running run attempt", runsView.Runs)
		}

		statusOutput, err = runOdinCommand(t, repoRoot, odinBinary, runtimeRoot, nil, "", "work", "status")
		if err != nil {
			t.Fatalf("runOdinCommand(work status after dispatch) error = %v\n%s", err, statusOutput)
		}
		for _, want := range []string{"work_items=1", "open_work_items=1", "active_run_attempts=1", "pending_approvals=0", "explicit_intent_work_items=1"} {
			if !strings.Contains(statusOutput, want) {
				t.Fatalf("work status after dispatch = %q, want %s", statusOutput, want)
			}
		}
	})

	t.Run("governance intent blocks through approval queue without run attempt", func(t *testing.T) {
		runtimeRoot := t.TempDir()

		startOutput, err := runOdinCommand(t, repoRoot, odinBinary, runtimeRoot, nil, "", "work", "start", "--project", "odin-core", "--title", "state contract approval proof", "--intent", "governance")
		if err != nil {
			t.Fatalf("runOdinCommand(work start governance) error = %v\n%s", err, startOutput)
		}
		if !strings.Contains(startOutput, "status=queued") || !strings.Contains(startOutput, "intent=governance") {
			t.Fatalf("governance start output = %q, want queued governance work item", startOutput)
		}

		dispatchOutput, err := runOdinCommand(t, repoRoot, odinBinary, runtimeRoot, nil, "", "work", "dispatch", "--task", "1", "--json")
		if err != nil {
			t.Fatalf("runOdinCommand(work dispatch governance) error = %v\n%s", err, dispatchOutput)
		}
		var dispatchView struct {
			Dispatched bool   `json:"dispatched"`
			Reason     string `json:"reason"`
			Task       struct {
				ID            int64  `json:"id"`
				Key           string `json:"key"`
				Status        string `json:"status"`
				BlockedReason string `json:"blocked_reason"`
			} `json:"task"`
			Run *struct {
				ID     int64  `json:"id"`
				Status string `json:"status"`
			} `json:"run"`
		}
		if err := json.Unmarshal([]byte(dispatchOutput), &dispatchView); err != nil {
			t.Fatalf("json.Unmarshal(governance dispatch output) error = %v\n%s", err, dispatchOutput)
		}
		if dispatchView.Dispatched || dispatchView.Reason != "approval_required" || dispatchView.Task.Status != "blocked" || dispatchView.Task.BlockedReason != "approval_required" || dispatchView.Run != nil {
			t.Fatalf("governance dispatch = %+v, want blocked approval without run attempt", dispatchView)
		}

		jobsOutput, err := runOdinCommand(t, repoRoot, odinBinary, runtimeRoot, nil, "", "jobs", "--json")
		if err != nil {
			t.Fatalf("runOdinCommand(jobs --json governance) error = %v\n%s", err, jobsOutput)
		}
		var jobsView struct {
			Jobs []struct {
				TaskKey       string `json:"task_key"`
				Status        string `json:"status"`
				BlockedReason string `json:"blocked_reason"`
			} `json:"jobs"`
		}
		if err := json.Unmarshal([]byte(jobsOutput), &jobsView); err != nil {
			t.Fatalf("json.Unmarshal(governance jobs output) error = %v\n%s", err, jobsOutput)
		}
		if len(jobsView.Jobs) != 1 || jobsView.Jobs[0].Status != "blocked" || jobsView.Jobs[0].BlockedReason != "approval_required" {
			t.Fatalf("governance jobs = %+v, want blocked work item with reason", jobsView.Jobs)
		}

		runsOutput, err := runOdinCommand(t, repoRoot, odinBinary, runtimeRoot, nil, "", "runs", "--json")
		if err != nil {
			t.Fatalf("runOdinCommand(runs --json governance) error = %v\n%s", err, runsOutput)
		}
		var runsView struct {
			Runs []struct {
				RunID  int64  `json:"run_id"`
				Status string `json:"status"`
			} `json:"runs"`
		}
		if err := json.Unmarshal([]byte(runsOutput), &runsView); err != nil {
			t.Fatalf("json.Unmarshal(governance runs output) error = %v\n%s", err, runsOutput)
		}
		if len(runsView.Runs) != 0 {
			t.Fatalf("governance runs = %+v, want no run attempts for approval-blocked work", runsView.Runs)
		}

		approvalsOutput, err := runOdinCommand(t, repoRoot, odinBinary, runtimeRoot, nil, "", "approvals", "all", "--json")
		if err != nil {
			t.Fatalf("runOdinCommand(approvals all --json) error = %v\n%s", err, approvalsOutput)
		}
		var approvalsView struct {
			Approvals []struct {
				TaskKey        string   `json:"task_key"`
				Status         string   `json:"status"`
				Risk           string   `json:"risk"`
				Reason         string   `json:"reason"`
				AllowedActions []string `json:"allowed_actions"`
			} `json:"approvals"`
		}
		if err := json.Unmarshal([]byte(approvalsOutput), &approvalsView); err != nil {
			t.Fatalf("json.Unmarshal(approvals output) error = %v\n%s", err, approvalsOutput)
		}
		if len(approvalsView.Approvals) != 1 {
			t.Fatalf("approvals = %+v, want one approval request", approvalsView.Approvals)
		}
		approval := approvalsView.Approvals[0]
		if approval.TaskKey != dispatchView.Task.Key || approval.Status != "pending" || approval.Risk != "governance" || approval.Reason != "approval_required" || !containsString(approval.AllowedActions, "approve") || !containsString(approval.AllowedActions, "deny") {
			t.Fatalf("approval = %+v, want pending governance approval for blocked work item", approval)
		}

		statusOutput, err := runOdinCommand(t, repoRoot, odinBinary, runtimeRoot, nil, "", "work", "status")
		if err != nil {
			t.Fatalf("runOdinCommand(work status governance) error = %v\n%s", err, statusOutput)
		}
		for _, want := range []string{"work_items=1", "open_work_items=1", "active_run_attempts=0", "pending_approvals=1", "explicit_intent_work_items=1"} {
			if !strings.Contains(statusOutput, want) {
				t.Fatalf("governance work status = %q, want %s", statusOutput, want)
			}
		}
	})
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
