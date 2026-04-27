package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	legacyobs "odin-os/internal/runtime/legacy"
)

func TestRunLegacyWithServiceRendersTextStatus(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	service := legacyobs.Service{
		Root: root,
		Runner: commandFakeRunner{
			commandKey("systemctl", "list-units", "--all", "--type=service", "--type=timer", "--no-legend", "--plain", "odin*"): {
				output: "odin-engine.service loaded active running Odin Engine\n",
			},
			commandKey("systemctl", "--user", "list-units", "--all", "--type=service", "--type=timer", "--no-legend", "--plain", "odin*"): {
				output: "",
			},
			commandKey("tmux", "list-sessions", "-F", "#{session_name}|#{session_attached}"): {
				output: "",
			},
		},
	}

	var output bytes.Buffer
	if err := RunLegacyWithService(context.Background(), service, []string{"status"}, &output); err != nil {
		t.Fatalf("RunLegacyWithService(status) error = %v", err)
	}
	if !strings.Contains(output.String(), "legacy_odin status=healthy") {
		t.Fatalf("output = %q, want legacy status", output.String())
	}
	if !strings.Contains(output.String(), "legacy_service scope=root service=odin-engine.service") {
		t.Fatalf("output = %q, want service row", output.String())
	}
}

func TestRunLegacyWithServiceRendersJSONStatus(t *testing.T) {
	t.Parallel()

	service := legacyobs.Service{
		Root: filepath.Join(t.TempDir(), "missing"),
		Runner: commandFakeRunner{
			commandKey("systemctl", "list-units", "--all", "--type=service", "--type=timer", "--no-legend", "--plain", "odin*"): {
				output: "",
			},
			commandKey("systemctl", "--user", "list-units", "--all", "--type=service", "--type=timer", "--no-legend", "--plain", "odin*"): {
				output: "",
			},
			commandKey("tmux", "list-sessions", "-F", "#{session_name}|#{session_attached}"): {
				output: "",
			},
		},
	}

	var output bytes.Buffer
	if err := RunLegacyWithService(context.Background(), service, []string{"status", "--json"}, &output); err != nil {
		t.Fatalf("RunLegacyWithService(status --json) error = %v", err)
	}

	var report struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatalf("json.Unmarshal() error = %v\n%s", err, output.String())
	}
	if report.Status != string(legacyobs.StatusMissing) {
		t.Fatalf("Status = %q, want %q", report.Status, legacyobs.StatusMissing)
	}
}

func TestRunLegacyWithServiceRendersCapabilityRegistry(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	scriptsRoot := filepath.Join(t.TempDir(), "scripts", "odin")
	if err := os.MkdirAll(filepath.Join(scriptsRoot, "config"), 0o755); err != nil {
		t.Fatalf("MkdirAll(config) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "routing.json"), []byte(`{"task_routing":{"server_health":"codex"},"role_defaults":{"ops":{}},"backends":{"codex":{}}}`), 0o644); err != nil {
		t.Fatalf("WriteFile(routing.json) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(scriptsRoot, "config", "schedules.json"), []byte(`[]`), 0o644); err != nil {
		t.Fatalf("WriteFile(schedules.json) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(scriptsRoot, "config", "tool-registry.json"), []byte(`{"tools":[]}`), 0o644); err != nil {
		t.Fatalf("WriteFile(tool-registry.json) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(scriptsRoot, "config", "task-type-registry.json"), []byte(`[{"type":"server_health","status":"active","role":"ops"}]`), 0o644); err != nil {
		t.Fatalf("WriteFile(task-type-registry.json) error = %v", err)
	}

	service := legacyobs.Service{
		Root:        root,
		ScriptsRoot: scriptsRoot,
		Runner: commandFakeRunner{
			commandKey("systemctl", "list-units", "--all", "--type=service", "--type=timer", "--no-legend", "--plain", "odin*"): {
				output: "odin-slack-gateway.service loaded active running Odin Slack Inbox Gateway\n",
			},
			commandKey("systemctl", "--user", "list-units", "--all", "--type=service", "--type=timer", "--no-legend", "--plain", "odin*"): {
				output: "",
			},
			commandKey("tmux", "list-sessions", "-F", "#{session_name}|#{session_attached}"): {
				output: "",
			},
		},
	}

	var output bytes.Buffer
	if err := RunLegacyWithService(context.Background(), service, []string{"capabilities"}, &output); err != nil {
		t.Fatalf("RunLegacyWithService(capabilities) error = %v", err)
	}
	for _, want := range []string{
		"legacy_capability_registry",
		"legacy_capability key=slack_intake source=service:odin-slack-gateway.service owner=provider_adapter classification=keep_until_migrated",
		"legacy_capability key=task_type:server_health source=config:task-type-registry.json owner=workflow classification=migrate",
		"legacy_capability key=task_route:server_health source=routing:task_routing owner=workflow classification=migrate",
	} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("output = %q, want %q", output.String(), want)
		}
	}
}

type commandFakeRunner map[string]commandFake

type commandFake struct {
	output string
	err    error
}

func (runner commandFakeRunner) CombinedOutput(_ context.Context, name string, args ...string) ([]byte, error) {
	command, ok := runner[commandKey(name, args...)]
	if !ok {
		return nil, nil
	}
	return []byte(command.output), command.err
}

func commandKey(name string, args ...string) string {
	return name + "\x00" + strings.Join(args, "\x00")
}
