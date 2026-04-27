package legacy

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReportCollectsLegacyRuntimeStateReadOnly(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeLegacyEngineDB(t, root)
	logPath := filepath.Join(root, "logs", "2026-04-24", "odin-strategist-1.log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(log dir) error = %v", err)
	}
	if err := os.WriteFile(logPath, []byte("starting strategist\nstall detected\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(log) error = %v", err)
	}
	checkpointDir := filepath.Join(root, "agents", "strategist-1")
	if err := os.MkdirAll(checkpointDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(checkpoint dir) error = %v", err)
	}
	checkpointJSON := `{
		"worker_id":"strategist-1",
		"task_id":"strategic_review-1",
		"lease_id":"lease-1",
		"backend":"claude",
		"task_type":"strategic_review",
		"repo_root":"/home/orchestrator/odin-orchestrator",
		"log_path":"` + logPath + `",
		"start_at":"2026-04-24T03:00:00Z",
		"ttl_seconds":3600
	}`
	if err := os.WriteFile(filepath.Join(checkpointDir, "checkpoint.json"), []byte(checkpointJSON), 0o644); err != nil {
		t.Fatalf("WriteFile(checkpoint.json) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "state.json"), []byte(`{"tasks_this_session":2,"dispatched_tasks_count":15,"active_runs":{"run-1":true}}`), 0o644); err != nil {
		t.Fatalf("WriteFile(state.json) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "routing.json"), []byte(`{"system_default":"claude","backends":{"claude":{},"codex":{}},"task_routing":{"server_health":"codex","pr_review":"claude"},"role_defaults":{"ops":{},"worker":{}}}`), 0o644); err != nil {
		t.Fatalf("WriteFile(routing.json) error = %v", err)
	}

	service := Service{
		Root: root,
		Runner: fakeRunner{
			key("systemctl", "list-units", "--all", "--type=service", "--type=timer", "--no-legend", "--plain", "odin*"): {
				output: "odin-engine.service loaded active running Odin Engine\nodin.service loaded failed failed Odin Dispatch Loop\n",
			},
			key("systemctl", "--user", "list-units", "--all", "--type=service", "--type=timer", "--no-legend", "--plain", "odin*"): {
				output: "odin-ohs.service loaded active running Odin OHS\n",
			},
			key("tmux", "list-sessions", "-F", "#{session_name}|#{session_attached}"): {
				output: "odin-strategist-1|0\nunrelated|1\n",
			},
		},
		Now: func() time.Time { return time.Date(2026, 4, 24, 3, 30, 0, 0, time.UTC) },
	}

	report, err := service.Report(context.Background())
	if err != nil {
		t.Fatalf("Report() error = %v", err)
	}
	if report.Status != StatusDegraded {
		t.Fatalf("Status = %q, want %q", report.Status, StatusDegraded)
	}
	if !report.RootExists {
		t.Fatalf("RootExists = false, want true")
	}
	if len(report.Services) != 3 {
		t.Fatalf("Services len = %d, want 3", len(report.Services))
	}
	if report.Engine.TaskCounts["completed"] != 2 || report.Engine.RunCounts["running"] != 1 {
		t.Fatalf("Engine counts = %+v/%+v, want completed tasks and running runs", report.Engine.TaskCounts, report.Engine.RunCounts)
	}
	if len(report.Engine.ActiveLeases) != 1 || report.Engine.ActiveLeases[0].WorkerID != "strategist-1" {
		t.Fatalf("ActiveLeases = %+v, want strategist lease", report.Engine.ActiveLeases)
	}
	if report.State.DispatchedTasksCount != 15 || report.State.ActiveRunsCount != 1 {
		t.Fatalf("State = %+v, want dispatched and active run counts", report.State)
	}
	if report.Routing.TaskRoutingCount != 2 || report.Routing.RoleDefaultCount != 2 {
		t.Fatalf("Routing = %+v, want route and role counts", report.Routing)
	}
	if len(report.Tmux) != 1 || report.Tmux[0].Name != "odin-strategist-1" {
		t.Fatalf("Tmux = %+v, want odin session only", report.Tmux)
	}
	if len(report.Checkpoints) != 1 || report.Checkpoints[0].WorkerID != "strategist-1" {
		t.Fatalf("Checkpoints = %+v, want strategist checkpoint", report.Checkpoints)
	}
	if report.Checkpoints[0].Log.LastLine != "stall detected" {
		t.Fatalf("Checkpoint log = %+v, want last line evidence", report.Checkpoints[0].Log)
	}

	rendered := RenderText(report)
	for _, want := range []string{
		"legacy_odin status=degraded",
		"legacy_engine",
		"legacy_service scope=root service=odin-engine.service",
		"legacy_tmux session=odin-strategist-1 attached=false",
		"legacy_checkpoint worker=strategist-1 task=strategic_review-1 backend=claude task_type=strategic_review",
		"legacy_checkpoint_log worker=strategist-1 exists=true last_line=\"stall detected\"",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("RenderText() = %q, want %q", rendered, want)
		}
	}
}

func TestReportMissingRootDoesNotFail(t *testing.T) {
	t.Parallel()

	service := Service{
		Root: filepath.Join(t.TempDir(), "missing"),
		Runner: fakeRunner{
			key("systemctl", "list-units", "--all", "--type=service", "--type=timer", "--no-legend", "--plain", "odin*"): {
				output: "",
			},
			key("systemctl", "--user", "list-units", "--all", "--type=service", "--type=timer", "--no-legend", "--plain", "odin*"): {
				output: "",
			},
			key("tmux", "list-sessions", "-F", "#{session_name}|#{session_attached}"): {
				output: "no server running on /tmp/tmux\n",
				err:    errors.New("exit status 1"),
			},
		},
	}

	report, err := service.Report(context.Background())
	if err != nil {
		t.Fatalf("Report() error = %v", err)
	}
	if report.Status != StatusMissing {
		t.Fatalf("Status = %q, want %q", report.Status, StatusMissing)
	}
	if report.RootExists {
		t.Fatalf("RootExists = true, want false")
	}
}

func TestLastNonEmptyLineSkipsShellPromptControlNoise(t *testing.T) {
	t.Parallel()

	content := "worker started\nstall detected\n\x1b[?25h\x1b[01;32morchestrator@gollahon-nas\x1b[00m:\x1b[01;34m~/odin-orchestrator\x1b[00m$\n"
	if got := lastNonEmptyLine(content); got != "stall detected" {
		t.Fatalf("lastNonEmptyLine() = %q, want stall detected", got)
	}
}

func TestCapabilityRegistryClassifiesLegacyParitySources(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	scriptsRoot := filepath.Join(t.TempDir(), "scripts", "odin")
	if err := os.MkdirAll(filepath.Join(scriptsRoot, "config"), 0o755); err != nil {
		t.Fatalf("MkdirAll(config) error = %v", err)
	}
	writeJSONFile(t, filepath.Join(root, "routing.json"), `{
		"backends":{"claude":{},"codex":{}},
		"task_routing":{"server_health":"codex","pr_review":"claude"},
		"role_defaults":{"ops":{},"worker":{}}
	}`)
	writeJSONFile(t, filepath.Join(scriptsRoot, "config", "schedules.json"), `[
		{"id":"morning-observer","type":"morning_observer","enabled":true},
		{"id":"disabled","type":"disabled_task","enabled":false}
	]`)
	writeJSONFile(t, filepath.Join(scriptsRoot, "config", "tool-registry.json"), `{
		"tools":[{"tool_id":"repo.read"},{"tool_id":"test.run"}]
	}`)
	writeJSONFile(t, filepath.Join(scriptsRoot, "config", "task-type-registry.json"), `[
		{"type":"server_health","status":"active","role":"ops","gemma4_eligible":true},
		{"type":"old_task","status":"deprecated","role":"worker","gemma4_eligible":false}
	]`)

	service := Service{
		Root:        root,
		ScriptsRoot: scriptsRoot,
		Runner: fakeRunner{
			key("systemctl", "list-units", "--all", "--type=service", "--type=timer", "--no-legend", "--plain", "odin*"): {
				output: "odin-slack-gateway.service loaded active running Odin Slack Inbox Gateway\nodin-keepalive.timer loaded active running Odin Keepalive Watchdog Timer\n",
			},
			key("systemctl", "--user", "list-units", "--all", "--type=service", "--type=timer", "--no-legend", "--plain", "odin*"): {
				output: "odin-ohs.service loaded active running Odin HTTP Hook Server\n",
			},
			key("tmux", "list-sessions", "-F", "#{session_name}|#{session_attached}"): {
				output: "",
			},
		},
	}

	report, err := service.Capabilities(context.Background())
	if err != nil {
		t.Fatalf("Capabilities() error = %v", err)
	}
	if report.Summary.TaskRoutes != 2 || report.Summary.RoleDefaults != 2 || report.Summary.Backends != 2 {
		t.Fatalf("Summary routing counts = %+v, want task/role/backend counts", report.Summary)
	}
	if report.Summary.EnabledSchedules != 1 || report.Summary.Tools != 2 {
		t.Fatalf("Summary registry counts = %+v, want enabled schedule and tool counts", report.Summary)
	}
	if report.Summary.TaskTypes != 2 || report.Summary.ActiveTaskTypes != 1 {
		t.Fatalf("Summary task type counts = %+v, want task type and active counts", report.Summary)
	}

	assertCapability(t, report, "slack_intake", "service:odin-slack-gateway.service", "provider_adapter", "keep_until_migrated")
	assertCapability(t, report, "webhook_intake", "service:odin-ohs.service", "provider_adapter", "keep_until_migrated")
	assertCapability(t, report, "keepalive_watchdog", "service:odin-keepalive.timer", "observability", "bridge_then_migrate")
	assertCapability(t, report, "task_route:server_health", "routing:task_routing", "workflow", "migrate")
	assertCapability(t, report, "role:ops", "routing:role_defaults", "worker", "migrate")
	assertCapability(t, report, "backend:claude", "routing:backends", "provider_adapter", "migrate")
	assertCapability(t, report, "schedule:morning-observer", "config:schedules.json", "automation_trigger", "migrate")
	assertCapability(t, report, "tool:repo.read", "config:tool-registry.json", "tool", "inventory_before_migration")
	assertCapability(t, report, "task_type:server_health", "config:task-type-registry.json", "workflow", "migrate")
	assertCapability(t, report, "task_type:old_task", "config:task-type-registry.json", "workflow", "inventory_before_migration")

	rendered := RenderCapabilityText(report)
	for _, want := range []string{
		"legacy_capability_registry",
		"task_routes=2",
		"task_types=2 active_task_types=1",
		"legacy_capability key=slack_intake source=service:odin-slack-gateway.service owner=provider_adapter classification=keep_until_migrated",
		"legacy_capability key=task_type:server_health source=config:task-type-registry.json owner=workflow classification=migrate",
		"legacy_capability key=task_route:server_health source=routing:task_routing owner=workflow classification=migrate",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("RenderCapabilityText() = %q, want %q", rendered, want)
		}
	}
}

func writeLegacyEngineDB(t *testing.T, root string) {
	t.Helper()

	db, err := sql.Open("sqlite", filepath.Join(root, "engine.db"))
	if err != nil {
		t.Fatalf("sql.Open(engine.db) error = %v", err)
	}
	defer db.Close()

	statements := []string{
		`CREATE TABLE tasks (id INTEGER PRIMARY KEY, status TEXT NOT NULL)`,
		`INSERT INTO tasks(status) VALUES ('completed'), ('completed'), ('failed')`,
		`CREATE TABLE runs (id INTEGER PRIMARY KEY, state TEXT NOT NULL)`,
		`INSERT INTO runs(state) VALUES ('running'), ('completed')`,
		`CREATE TABLE leases (id TEXT, task_id TEXT, agent_id TEXT, status TEXT, released INTEGER, expires_at TEXT, last_heartbeat TEXT)`,
		`INSERT INTO leases(id, task_id, agent_id, status, released, expires_at, last_heartbeat) VALUES ('lease-1', 'task-1', 'strategist-1', 'active', 0, '2026-04-24T04:00:00Z', '2026-04-24T03:00:00Z')`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("Exec(%q) error = %v", statement, err)
		}
	}
}

func writeJSONFile(t *testing.T, path string, content string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", path, err)
	}
}

func assertCapability(t *testing.T, report CapabilityReport, key string, source string, owner string, classification string) {
	t.Helper()

	for _, capability := range report.Capabilities {
		if capability.Key == key {
			if capability.Source != source || capability.Owner != owner || capability.Classification != classification {
				t.Fatalf("capability %s = %+v, want source=%s owner=%s classification=%s", key, capability, source, owner, classification)
			}
			return
		}
	}
	t.Fatalf("capability %s missing from %+v", key, report.Capabilities)
}

type fakeRunner map[string]fakeCommand

type fakeCommand struct {
	output string
	err    error
}

func (runner fakeRunner) CombinedOutput(_ context.Context, name string, args ...string) ([]byte, error) {
	command, ok := runner[key(name, args...)]
	if !ok {
		return nil, nil
	}
	return []byte(command.output), command.err
}

func key(name string, args ...string) string {
	return name + "\x00" + strings.Join(args, "\x00")
}
