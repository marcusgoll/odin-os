package lifecycle

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"odin-os/internal/store/sqlite"
)

func TestRunGoalCreateAndListJSONPersistsAcrossRuns(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := testRepoRoot(t)

	var createOut bytes.Buffer
	if err := Run(context.Background(), root, []string{"goal", "create", "--title", "test", "--json"}, strings.NewReader(""), &createOut); err != nil {
		t.Fatalf("Run(goal create) error = %v", err)
	}

	var created struct {
		Goal struct {
			ID     int64  `json:"id"`
			Title  string `json:"title"`
			Status string `json:"status"`
		} `json:"goal"`
	}
	if err := json.Unmarshal(createOut.Bytes(), &created); err != nil {
		t.Fatalf("goal create json decode error = %v; output=%s", err, createOut.String())
	}
	if created.Goal.ID == 0 || created.Goal.Title != "test" || created.Goal.Status != string(sqlite.GoalStatusCreated) {
		t.Fatalf("created goal = %+v, want persisted created goal", created.Goal)
	}

	var listOut bytes.Buffer
	if err := Run(context.Background(), root, []string{"goal", "list", "--json"}, strings.NewReader(""), &listOut); err != nil {
		t.Fatalf("Run(goal list) error = %v", err)
	}
	var listed struct {
		Goals []struct {
			ID     int64  `json:"id"`
			Title  string `json:"title"`
			Status string `json:"status"`
		} `json:"goals"`
	}
	if err := json.Unmarshal(listOut.Bytes(), &listed); err != nil {
		t.Fatalf("goal list json decode error = %v; output=%s", err, listOut.String())
	}
	if len(listed.Goals) != 1 {
		t.Fatalf("listed goals len = %d, want 1", len(listed.Goals))
	}
	if listed.Goals[0].ID != created.Goal.ID || listed.Goals[0].Status != string(sqlite.GoalStatusCreated) {
		t.Fatalf("listed goal = %+v, want created goal id=%d", listed.Goals[0], created.Goal.ID)
	}

	store, err := sqlite.Open(filepath.Join(root, "data", "odin.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()
	events, err := store.ListEvents(context.Background(), sqlite.ListEventsParams{})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	found := false
	for _, event := range events {
		if event.StreamType == "goal" && event.Type == "goal.created" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("goal.created audit event missing from %d events", len(events))
	}

	if _, err := os.Stat(filepath.Join(root, "data", "odin.db")); err != nil {
		t.Fatalf("runtime DB stat error = %v", err)
	}
}

func TestRunGoalShowUpdateTransitionAndInvalidTransition(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := testRepoRoot(t)

	var createOut bytes.Buffer
	if err := Run(context.Background(), root, []string{"goal", "create", "--title", "cli hardening test", "--description", "first pass", "--json"}, strings.NewReader(""), &createOut); err != nil {
		t.Fatalf("Run(goal create) error = %v", err)
	}
	created := decodeGoalEnvelope(t, createOut.Bytes())

	var showOut bytes.Buffer
	if err := Run(context.Background(), root, []string{"goal", "show", "--id", int64String(created.ID), "--json"}, strings.NewReader(""), &showOut); err != nil {
		t.Fatalf("Run(goal show) error = %v", err)
	}
	shown := decodeGoalEnvelope(t, showOut.Bytes())
	if shown.ID != created.ID || shown.Title != "cli hardening test" {
		t.Fatalf("shown goal = %+v, want created goal id/title", shown)
	}

	var updateOut bytes.Buffer
	if err := Run(context.Background(), root, []string{"goal", "update", "--id", int64String(created.ID), "--title", "updated title", "--description", "updated description", "--json"}, strings.NewReader(""), &updateOut); err != nil {
		t.Fatalf("Run(goal update) error = %v", err)
	}
	updated := decodeGoalEnvelope(t, updateOut.Bytes())
	if updated.Title != "updated title" || updated.Description != "updated description" {
		t.Fatalf("updated goal = %+v, want updated title and description", updated)
	}

	var transitionOut bytes.Buffer
	if err := Run(context.Background(), root, []string{"goal", "transition", "--id", int64String(created.ID), "--status", "planned", "--json"}, strings.NewReader(""), &transitionOut); err != nil {
		t.Fatalf("Run(goal transition planned) error = %v", err)
	}
	planned := decodeGoalEnvelope(t, transitionOut.Bytes())
	if planned.Status != string(sqlite.GoalStatusPlanned) {
		t.Fatalf("planned.Status = %q, want planned", planned.Status)
	}

	err := Run(context.Background(), root, []string{"goal", "transition", "--id", int64String(created.ID), "--status", "running", "--json"}, strings.NewReader(""), &bytes.Buffer{})
	if !errors.Is(err, sqlite.ErrInvalidGoalTransition) {
		t.Fatalf("Run(goal invalid transition) error = %v, want %v", err, sqlite.ErrInvalidGoalTransition)
	}

	var listOut bytes.Buffer
	if err := Run(context.Background(), root, []string{"goal", "list", "--status", "planned", "--limit", "1", "--json"}, strings.NewReader(""), &listOut); err != nil {
		t.Fatalf("Run(goal list filter) error = %v", err)
	}
	var listed struct {
		Goals []goalJSON `json:"goals"`
	}
	if err := json.Unmarshal(listOut.Bytes(), &listed); err != nil {
		t.Fatalf("goal list json decode error = %v; output=%s", err, listOut.String())
	}
	if len(listed.Goals) != 1 || listed.Goals[0].ID != created.ID || listed.Goals[0].Status != string(sqlite.GoalStatusPlanned) {
		t.Fatalf("listed goals = %+v, want one planned goal %d", listed.Goals, created.ID)
	}

	store, err := sqlite.Open(filepath.Join(root, "data", "odin.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()
	events, err := store.ListEvents(context.Background(), sqlite.ListEventsParams{})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	counts := map[string]int{}
	for _, event := range events {
		if event.StreamType == "goal" {
			counts[string(event.Type)]++
		}
	}
	if counts["goal.created"] != 1 || counts["goal.updated"] != 1 || counts["goal.status_changed"] != 1 {
		t.Fatalf("goal event counts = %#v, want created/update/status_changed", counts)
	}
}

func TestRunGoalTickJSONStartsApprovedGoalAndAudits(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := testRepoRoot(t)

	var createOut bytes.Buffer
	if err := Run(context.Background(), root, []string{"goal", "create", "--title", "runner tick test", "--json"}, strings.NewReader(""), &createOut); err != nil {
		t.Fatalf("Run(goal create) error = %v", err)
	}
	created := decodeGoalEnvelope(t, createOut.Bytes())
	for _, status := range []string{"planned", "approved_for_execution"} {
		if err := Run(context.Background(), root, []string{"goal", "transition", "--id", int64String(created.ID), "--status", status, "--json"}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
			t.Fatalf("Run(goal transition %s) error = %v", status, err)
		}
	}

	var tickOut bytes.Buffer
	if err := Run(context.Background(), root, []string{"goal", "tick", "--json"}, strings.NewReader(""), &tickOut); err != nil {
		t.Fatalf("Run(goal tick) error = %v", err)
	}
	var ticked struct {
		Observed int `json:"observed"`
		Started  int `json:"started"`
		Blocked  int `json:"blocked"`
		Skipped  int `json:"skipped"`
		Results  []struct {
			GoalID    int64  `json:"goal_id"`
			Action    string `json:"action"`
			Reason    string `json:"reason"`
			GoalRunID *int64 `json:"goal_run_id,omitempty"`
		} `json:"results"`
	}
	if err := json.Unmarshal(tickOut.Bytes(), &ticked); err != nil {
		t.Fatalf("goal tick json decode error = %v; output=%s", err, tickOut.String())
	}
	if ticked.Observed != 1 || ticked.Started != 1 || ticked.Blocked != 0 || ticked.Skipped != 0 {
		t.Fatalf("tick result = %+v, want one approved goal started", ticked)
	}
	if len(ticked.Results) != 1 || ticked.Results[0].GoalID != created.ID || ticked.Results[0].Action != "started" || ticked.Results[0].GoalRunID == nil {
		t.Fatalf("tick results = %+v, want started goal run for goal %d", ticked.Results, created.ID)
	}

	var showOut bytes.Buffer
	if err := Run(context.Background(), root, []string{"goal", "show", "--id", int64String(created.ID), "--json"}, strings.NewReader(""), &showOut); err != nil {
		t.Fatalf("Run(goal show) error = %v", err)
	}
	shown := decodeGoalEnvelope(t, showOut.Bytes())
	if shown.Status != string(sqlite.GoalStatusRunning) {
		t.Fatalf("shown.Status = %q, want running after tick", shown.Status)
	}

	store, err := sqlite.Open(filepath.Join(root, "data", "odin.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()
	events, err := store.ListEvents(context.Background(), sqlite.ListEventsParams{})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	counts := map[string]int{}
	for _, event := range events {
		if event.StreamType == "goal" {
			counts[string(event.Type)]++
		}
	}
	if counts["goal_runner.observed"] != 1 || counts["goal_run.started"] != 1 {
		t.Fatalf("goal tick audit counts = %#v, want observed and run started", counts)
	}
}

type goalJSON struct {
	ID          int64  `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Status      string `json:"status"`
}

func decodeGoalEnvelope(t *testing.T, payload []byte) goalJSON {
	t.Helper()
	var envelope struct {
		Goal goalJSON `json:"goal"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		t.Fatalf("goal json decode error = %v; output=%s", err, string(payload))
	}
	return envelope.Goal
}

func int64String(value int64) string {
	return strconv.FormatInt(value, 10)
}
