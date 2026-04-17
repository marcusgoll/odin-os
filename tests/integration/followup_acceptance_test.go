package integration_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	memoryroot "odin-os/internal/memory"
	"odin-os/internal/store/sqlite"
)

func TestFollowupAcceptance(t *testing.T) {
	ctx := context.Background()
	repoRoot := projectRoot(t)
	odinBinary := buildOdinBinary(t, repoRoot)

	t.Run("marcus routine workflow stays durable and governed", func(t *testing.T) {
		runtimeRoot := t.TempDir()

		initiativeOutput, err := runOdinCommand(t, repoRoot, odinBinary, runtimeRoot, nil, "", "initiative", "create", "--kind", "routine", "--key", "life-admin", "--title", "Life Admin")
		if err != nil {
			t.Fatalf("runOdinCommand(initiative create) error = %v\n%s", err, initiativeOutput)
		}
		if !strings.Contains(initiativeOutput, "life-admin") {
			t.Fatalf("initiative create output = %q, want life-admin", initiativeOutput)
		}

		companionOutput, err := runOdinCommand(t, repoRoot, odinBinary, runtimeRoot, nil, "", "companion", "create", "--kind", "advisor", "--key", "finance", "--title", "Finance Advisor")
		if err != nil {
			t.Fatalf("runOdinCommand(companion create) error = %v\n%s", err, companionOutput)
		}
		if !strings.Contains(companionOutput, "finance") {
			t.Fatalf("companion create output = %q, want finance", companionOutput)
		}

		profileOutput, err := runOdinCommand(t, repoRoot, odinBinary, runtimeRoot, nil, "", "profile", "set", "--quiet-hours", "22:00-07:00")
		if err != nil {
			t.Fatalf("runOdinCommand(profile set) error = %v\n%s", err, profileOutput)
		}
		if !strings.Contains(profileOutput, "22:00-07:00") {
			t.Fatalf("profile set output = %q, want quiet hours", profileOutput)
		}

		followupOutput, err := runOdinCommand(t, repoRoot, odinBinary, runtimeRoot, nil, "", "followup", "add", "--initiative", "life-admin", "--title", "Review mail", "--cadence", "daily")
		if err != nil {
			t.Fatalf("runOdinCommand(followup add) error = %v\n%s", err, followupOutput)
		}
		if !strings.Contains(followupOutput, "created follow-up") {
			t.Fatalf("followup add output = %q, want created follow-up", followupOutput)
		}

		listOutput, err := runOdinCommand(t, repoRoot, odinBinary, runtimeRoot, nil, "", "followup", "list", "--json")
		if err != nil {
			t.Fatalf("runOdinCommand(followup list --json) error = %v\n%s", err, listOutput)
		}
		var followupsView struct {
			Obligations []struct {
				ID         int64     `json:"id"`
				Title      string    `json:"title"`
				Status     string    `json:"status"`
				NextDueAt  time.Time `json:"next_due_at"`
				Initiative string    `json:"initiative_key"`
			} `json:"obligations"`
		}
		if err := json.Unmarshal([]byte(listOutput), &followupsView); err != nil {
			t.Fatalf("json.Unmarshal(followup list) error = %v\n%s", err, listOutput)
		}
		if len(followupsView.Obligations) != 1 {
			t.Fatalf("followup list obligations len = %d, want 1", len(followupsView.Obligations))
		}
		followup := followupsView.Obligations[0]
		if followup.Initiative != "life-admin" || followup.Status != "active" {
			t.Fatalf("followup list obligation = %+v, want active life-admin obligation", followup)
		}

		fakeNow := followup.NextDueAt.Add(time.Hour).UTC().Format(time.RFC3339)
		agendaOutput, err := runOdinCommand(t, repoRoot, odinBinary, runtimeRoot, map[string]string{"ODIN_NOW": fakeNow}, "", "agenda", "--json")
		if err != nil {
			t.Fatalf("runOdinCommand(agenda --json) error = %v\n%s", err, agendaOutput)
		}
		var agendaView struct {
			DueWork []struct {
				Title     string `json:"title"`
				DueStatus string `json:"due_status"`
			} `json:"due_work"`
		}
		if err := json.Unmarshal([]byte(agendaOutput), &agendaView); err != nil {
			t.Fatalf("json.Unmarshal(agenda output) error = %v\n%s", err, agendaOutput)
		}
		if len(agendaView.DueWork) != 1 || agendaView.DueWork[0].Title != "Review mail" || agendaView.DueWork[0].DueStatus != "due" {
			t.Fatalf("agenda due work = %+v, want one due Review mail item", agendaView.DueWork)
		}

		store := openRuntimeStore(t, runtimeRoot)
		profileEntries, err := store.ListMemorySummaries(ctx, sqlite.ListMemorySummariesParams{
			Scope:      "workspace",
			ScopeKey:   "default",
			MemoryType: memoryroot.MemoryTypeOperatingProfileUpdate,
		})
		if err != nil {
			t.Fatalf("ListMemorySummaries(profile) error = %v", err)
		}
		if len(profileEntries) != 1 {
			t.Fatalf("profile memory len = %d, want 1", len(profileEntries))
		}
		if err := store.Close(); err != nil {
			t.Fatalf("Close(runtime store) error = %v", err)
		}

		serveCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		cmd := exec.CommandContext(serveCtx, odinBinary, "serve")
		cmd.Dir = repoRoot
		cmd.Env = append(os.Environ(),
			"ODIN_ROOT="+runtimeRoot,
			"ODIN_HTTP_ADDR=127.0.0.1:0",
			"ODIN_NOW="+fakeNow,
		)
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			t.Fatalf("StdoutPipe() error = %v", err)
		}
		stderr, err := cmd.StderrPipe()
		if err != nil {
			t.Fatalf("StderrPipe() error = %v", err)
		}
		if err := cmd.Start(); err != nil {
			t.Fatalf("cmd.Start() error = %v", err)
		}
		stopped := false
		defer func() {
			if stopped {
				return
			}
			_ = cmd.Process.Signal(os.Interrupt)
			_ = cmd.Wait()
		}()

		_ = waitForServeAddress(t, stdout, stderr)

		deadline := time.After(3 * time.Second)
		for {
			jobsOutput, err := runOdinCommand(t, repoRoot, odinBinary, runtimeRoot, nil, "", "jobs", "--json")
			if err != nil {
				t.Fatalf("runOdinCommand(jobs --json) error = %v\n%s", err, jobsOutput)
			}

			var jobsView struct {
				Jobs []struct {
					TaskKey string `json:"task_key"`
					Status  string `json:"status"`
				} `json:"jobs"`
			}
			if err := json.Unmarshal([]byte(jobsOutput), &jobsView); err != nil {
				t.Fatalf("json.Unmarshal(jobs output) error = %v\n%s", err, jobsOutput)
			}
			if len(jobsView.Jobs) == 1 && jobsView.Jobs[0].Status == "blocked" {
				break
			}

			select {
			case <-deadline:
				t.Fatalf("jobs output = %s, want one blocked materialized follow-up", jobsOutput)
			case <-time.After(100 * time.Millisecond):
			}
		}

		runsOutput, err := runOdinCommand(t, repoRoot, odinBinary, runtimeRoot, nil, "", "runs", "--json")
		if err != nil {
			t.Fatalf("runOdinCommand(runs --json) error = %v\n%s", err, runsOutput)
		}
		var runsView struct {
			Runs []struct {
				TaskKey string `json:"task_key"`
				Status  string `json:"status"`
			} `json:"runs"`
		}
		if err := json.Unmarshal([]byte(runsOutput), &runsView); err != nil {
			t.Fatalf("json.Unmarshal(runs output) error = %v\n%s", err, runsOutput)
		}
		if len(runsView.Runs) != 0 {
			t.Fatalf("runs view = %+v, want no runs for blocked bounded follow-up", runsView.Runs)
		}

		if err := cmd.Process.Signal(os.Interrupt); err != nil {
			t.Fatalf("Signal(os.Interrupt) error = %v", err)
		}
		if err := cmd.Wait(); err != nil {
			t.Fatalf("cmd.Wait() error = %v", err)
		}
		stopped = true
	})
}
