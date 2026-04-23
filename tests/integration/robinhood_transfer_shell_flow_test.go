package integration_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"odin-os/internal/store/sqlite"
)

func TestRobinhoodTransferShellFlowDeterministic(t *testing.T) {
	repoRoot := projectRoot(t)
	binaryPath := buildOdinBinary(t, repoRoot)

	t.Run("submitted", func(t *testing.T) {
		runtimeRoot := t.TempDir()
		driverPath := writeRobinhoodShellFixtureDriver(t, `{"status":"completed","tool_key":"robinhood_transfer_flow","summary":"Robinhood transfer submitted","artifacts":{"session_state":"submitted","current_url":"https://robinhood.com/transfers","next_action":"verify transfer status"}}`)
		output, err := runOdinCommand(t, repoRoot, binaryPath, runtimeRoot, map[string]string{
			"ODIN_HUGINN_ROBINHOOD_TRANSFER_DRIVER": driverPath,
		}, "/project pbs\n/transfer prepare direction=deposit amount_usd=25.00 source_account=checking destination_account=brokerage memo=test\n/runs show 1\n/approvals resolve 1 approve because final confirmation\n/runs show active\n", "repl")
		if err != nil {
			t.Fatalf("runOdinCommand(repl) error = %v\n%s", err, output)
		}

		for _, want := range []string{
			"task=robinhood-transfer-",
			"run=1 approval=1",
			"summary=review prepared and awaiting approval",
			"run=1 task=robinhood-transfer-",
			"artifact=driver_result summary=Robinhood transfer review ready",
			"approval=1 status=resolved result=approved run=2",
			"run=2 task=robinhood-transfer-",
			"status=completed executor=robinhood_transfer_submit",
			"summary=Robinhood transfer submitted",
			`"session_state":"submitted"`,
		} {
			if !strings.Contains(output, want) {
				t.Fatalf("output missing %q\n%s", want, output)
			}
		}

		store := openRuntimeStore(t, runtimeRoot)
		task, err := store.GetTask(context.Background(), 1)
		if err != nil {
			t.Fatalf("GetTask(1) error = %v", err)
		}
		if task.Status != "completed" {
			t.Fatalf("task.Status = %q, want completed", task.Status)
		}

		submitRun, err := store.GetRun(context.Background(), 2)
		if err != nil {
			t.Fatalf("GetRun(2) error = %v", err)
		}
		if submitRun.Status != "completed" {
			t.Fatalf("submitRun.Status = %q, want completed", submitRun.Status)
		}

		artifacts, err := store.ListRunArtifacts(context.Background(), sqlite.ListRunArtifactsParams{RunID: 2})
		if err != nil {
			t.Fatalf("ListRunArtifacts(run=2) error = %v", err)
		}
		if len(artifacts) != 1 {
			t.Fatalf("submit artifacts len = %d, want 1", len(artifacts))
		}
		if !strings.Contains(artifacts[0].DetailsJSON, `"session_state":"submitted"`) {
			t.Fatalf("submit artifact details = %q, want submitted state", artifacts[0].DetailsJSON)
		}

		taskID := int64(1)
		wakePackets, err := store.ListContextPackets(context.Background(), sqlite.ListContextPacketsParams{
			TaskID:      &taskID,
			PacketScope: "task_wake_packet",
		})
		if err != nil {
			t.Fatalf("ListContextPackets() error = %v", err)
		}
		if len(wakePackets) != 2 {
			t.Fatalf("wake packet count = %d, want 2", len(wakePackets))
		}
		if wakePackets[0].Status != "superseded" || wakePackets[0].Trigger != "approval_wait" {
			t.Fatalf("first wake packet = %+v, want superseded approval_wait", wakePackets[0])
		}
		if wakePackets[1].Status != "sealed" || wakePackets[1].Trigger != "completion" {
			t.Fatalf("second wake packet = %+v, want sealed completion", wakePackets[1])
		}
	})

	t.Run("resume verification failed", func(t *testing.T) {
		runtimeRoot := t.TempDir()
		driverPath := writeRobinhoodShellFixtureDriver(t, `{"status":"completed","tool_key":"robinhood_transfer_flow","summary":"Robinhood review continuity could not be verified","artifacts":{"session_state":"resume_verification_failed","prior_session_state":"session_expired","current_url":"https://robinhood.com/transfer","next_action":"fresh prepare required"}}`)
		output, err := runOdinCommand(t, repoRoot, binaryPath, runtimeRoot, map[string]string{
			"ODIN_HUGINN_ROBINHOOD_TRANSFER_DRIVER": driverPath,
		}, "/project pbs\n/transfer prepare direction=deposit amount_usd=25.00 source_account=checking destination_account=brokerage memo=test\n/approvals resolve 1 approve because final confirmation\n/runs show active\n", "repl")
		if err != nil {
			t.Fatalf("runOdinCommand(repl) error = %v\n%s", err, output)
		}

		for _, want := range []string{
			"task=robinhood-transfer-",
			"approval=1 status=resolved result=approved run=2",
			"run=2 task=robinhood-transfer-",
			"status=failed executor=robinhood_transfer_submit",
			"summary=Robinhood review continuity could not be verified",
			`"session_state":"resume_verification_failed"`,
			`"prior_session_state":"session_expired"`,
		} {
			if !strings.Contains(output, want) {
				t.Fatalf("output missing %q\n%s", want, output)
			}
		}

		store := openRuntimeStore(t, runtimeRoot)
		task, err := store.GetTask(context.Background(), 1)
		if err != nil {
			t.Fatalf("GetTask(1) error = %v", err)
		}
		if task.Status != "blocked" {
			t.Fatalf("task.Status = %q, want blocked", task.Status)
		}
		if task.TerminalReason != "stale_context" {
			t.Fatalf("task.TerminalReason = %q, want stale_context", task.TerminalReason)
		}

		submitRun, err := store.GetRun(context.Background(), 2)
		if err != nil {
			t.Fatalf("GetRun(2) error = %v", err)
		}
		if submitRun.Status != "failed" {
			t.Fatalf("submitRun.Status = %q, want failed", submitRun.Status)
		}
		if submitRun.TerminalReason != "resume_verification_failed" {
			t.Fatalf("submitRun.TerminalReason = %q, want resume_verification_failed", submitRun.TerminalReason)
		}

		artifacts, err := store.ListRunArtifacts(context.Background(), sqlite.ListRunArtifactsParams{RunID: 2})
		if err != nil {
			t.Fatalf("ListRunArtifacts(run=2) error = %v", err)
		}
		if len(artifacts) != 1 {
			t.Fatalf("submit artifacts len = %d, want 1", len(artifacts))
		}
		if !strings.Contains(artifacts[0].DetailsJSON, `"prior_session_state":"session_expired"`) {
			t.Fatalf("submit artifact details = %q, want prior_session_state", artifacts[0].DetailsJSON)
		}

		taskID := int64(1)
		activeWakePackets, err := store.ListContextPackets(context.Background(), sqlite.ListContextPacketsParams{
			TaskID:      &taskID,
			PacketScope: "task_wake_packet",
			Status:      "active",
		})
		if err != nil {
			t.Fatalf("ListContextPackets(active) error = %v", err)
		}
		if len(activeWakePackets) != 0 {
			t.Fatalf("active wake packets = %d, want 0", len(activeWakePackets))
		}

		wakePackets, err := store.ListContextPackets(context.Background(), sqlite.ListContextPacketsParams{
			TaskID:      &taskID,
			PacketScope: "task_wake_packet",
		})
		if err != nil {
			t.Fatalf("ListContextPackets() error = %v", err)
		}
		if len(wakePackets) != 1 {
			t.Fatalf("wake packet count = %d, want 1", len(wakePackets))
		}
		if wakePackets[0].Status != "sealed" || wakePackets[0].Trigger != "approval_wait" {
			t.Fatalf("wake packet = %+v, want sealed approval_wait", wakePackets[0])
		}
		if !strings.Contains(wakePackets[0].PayloadJSON, `"blocking_reason":"stale_context"`) {
			t.Fatalf("wake packet payload = %q, want stale_context", wakePackets[0].PayloadJSON)
		}
	})
}

func TestMarcusRobinhoodLiveTransferRunbookContainsDeterministicAndAttendedSteps(t *testing.T) {
	repoRoot := projectRoot(t)
	assertFileContains(t, filepath.Join(repoRoot, "docs", "operations", "marcus-robinhood-live-transfer-runbook.md"), []string{
		"deterministic proof",
		"testrobinhoodtransfershellflowdeterministic",
		"./bin/odin",
		"/project family-ops",
		"/transfer prepare direction=deposit amount_usd=1.00",
		"/approvals resolve <approval-id> approve because",
		"/runs show <submit-run-id>",
		"principal-attended",
		"unknown project: family-ops",
	})
}

func writeRobinhoodShellFixtureDriver(t *testing.T, submitJSON string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "robinhood-transfer-driver.sh")
script := fmt.Sprintf(`#!/usr/bin/env bash
set -euo pipefail
request_json="$(cat)"
mode="$(jq -r '.input.mode // "prepare"' <<<"$request_json")"
if [[ "$mode" == "prepare" ]]; then
  printf '%%s' '{"status":"completed","tool_key":"robinhood_transfer_flow","summary":"Robinhood transfer review ready","artifacts":{"session_state":"review_ready","current_url":"https://robinhood.com/transfer","next_action":"request approval"}}'
  exit 0
fi
cat <<'EOF'
%s
EOF
`, submitJSON)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile(driver) error = %v", err)
	}
	return path
}
