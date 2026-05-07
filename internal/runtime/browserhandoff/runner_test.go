package browserhandoff

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStubRunnerAcceptsValidStartRequestWithoutLaunchingProcess(t *testing.T) {
	runner := StubRunner{}
	response, err := runner.Start(context.Background(), StartRequest{
		SessionID:      1,
		LoginRequestID: 2,
		HandoffID:      "opaque-handoff-id",
		ProfilePath:    "browser-sessions/profiles/marcus-example",
		AllowedDomain:  "example.com",
		TimeoutSeconds: 600,
		BindAddr:       "127.0.0.1:0",
		PrivateBaseURL: "https://odin-handoff.tailnet.local",
		PublicBaseURL:  "",
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if response.Status != StatusNotImplemented {
		t.Fatalf("response.Status = %q, want %q", response.Status, StatusNotImplemented)
	}
	if response.SessionID != 1 || response.LoginRequestID != 2 || response.HandoffID != "opaque-handoff-id" {
		t.Fatalf("response = %+v, want linked request metadata", response)
	}
	if response.RunnerID != "" || response.ProcessID != 0 || response.ViewerURL != "" {
		t.Fatalf("response = %+v, want no runner/process/viewer metadata from stub", response)
	}
	if runner.LaunchCount() != 0 {
		t.Fatalf("LaunchCount() = %d, want 0", runner.LaunchCount())
	}
}

func TestStubRunnerRejectsMissingRequiredStartRequestFields(t *testing.T) {
	valid := StartRequest{
		SessionID:      1,
		LoginRequestID: 2,
		HandoffID:      "opaque-handoff-id",
		ProfilePath:    "browser-sessions/profiles/marcus-example",
		AllowedDomain:  "example.com",
		TimeoutSeconds: 600,
	}
	tests := []struct {
		name    string
		mutate  func(*StartRequest)
		wantErr string
	}{
		{name: "session id", mutate: func(request *StartRequest) { request.SessionID = 0 }, wantErr: "session_id"},
		{name: "login request id", mutate: func(request *StartRequest) { request.LoginRequestID = 0 }, wantErr: "login_request_id"},
		{name: "handoff id", mutate: func(request *StartRequest) { request.HandoffID = "" }, wantErr: "handoff_id"},
		{name: "profile path", mutate: func(request *StartRequest) { request.ProfilePath = "" }, wantErr: "profile_path"},
		{name: "allowed domain", mutate: func(request *StartRequest) { request.AllowedDomain = "" }, wantErr: "allowed_domain"},
		{name: "timeout", mutate: func(request *StartRequest) { request.TimeoutSeconds = 0 }, wantErr: "timeout_seconds"},
		{name: "absolute profile path", mutate: func(request *StartRequest) { request.ProfilePath = "/tmp/browser-profile" }, wantErr: "profile_path"},
		{name: "profile path traversal", mutate: func(request *StartRequest) { request.ProfilePath = "browser-sessions/profiles/../escape" }, wantErr: "profile_path"},
		{name: "domain with scheme", mutate: func(request *StartRequest) { request.AllowedDomain = "https://example.com" }, wantErr: "allowed_domain"},
		{name: "public bind address", mutate: func(request *StartRequest) { request.BindAddr = "0.0.0.0:5901" }, wantErr: "bind_addr"},
		{name: "public base url", mutate: func(request *StartRequest) { request.PublicBaseURL = "https://example.com" }, wantErr: "public_base_url"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := valid
			test.mutate(&request)
			_, err := StubRunner{}.Start(context.Background(), request)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("Start() error = %v, want substring %q", err, test.wantErr)
			}
		})
	}
}

func TestStubRunnerCancelReturnsStructuredResponse(t *testing.T) {
	response, err := StubRunner{}.Cancel(context.Background(), CancelRequest{
		RunnerID: "browser-handoff-runner-1",
		Reason:   "operator cancelled",
	})
	if err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}
	if response.RunnerID != "browser-handoff-runner-1" || response.Status != StatusNotImplemented {
		t.Fatalf("Cancel() = %+v, want structured not implemented response", response)
	}
	if response.ErrorCode != "not_implemented" {
		t.Fatalf("ErrorCode = %q, want not_implemented", response.ErrorCode)
	}

	_, err = StubRunner{}.Cancel(context.Background(), CancelRequest{})
	if err == nil || !strings.Contains(err.Error(), "runner_id") {
		t.Fatalf("Cancel(missing runner id) error = %v, want runner_id rejection", err)
	}
}

func TestRunnerFromEnvDefaultsToStubRunner(t *testing.T) {
	t.Setenv(RunnerModeEnvVar, "")
	t.Setenv(FixtureCommandEnvVar, testExecutablePath(t, "true"))
	t.Setenv(FixtureAllowedCommandsEnvVar, testExecutablePath(t, "true"))

	runner, err := RunnerFromEnv()
	if err != nil {
		t.Fatalf("RunnerFromEnv() error = %v", err)
	}
	if _, ok := runner.(StubRunner); !ok {
		t.Fatalf("RunnerFromEnv() = %T, want StubRunner by default", runner)
	}
}

func TestRunnerFromEnvSelectsNoVNCRunner(t *testing.T) {
	t.Setenv(RunnerModeEnvVar, RunnerModeNoVNC)

	runner, err := RunnerFromEnv()
	if err != nil {
		t.Fatalf("RunnerFromEnv() error = %v", err)
	}
	if _, ok := runner.(NoVNCRunner); !ok {
		t.Fatalf("RunnerFromEnv() = %T, want NoVNCRunner", runner)
	}
}

func TestNoVNCRunnerStartLaunchesFixtureProcessesAndCompletes(t *testing.T) {
	commandPath := testExecutablePath(t, "true")
	supervisor := &fakeNoVNCProcessSupervisor{
		results: map[string]ProcessStatus{
			"display": ProcessStatusExited,
			"browser": ProcessStatusExited,
			"novnc":   ProcessStatusExited,
		},
	}
	runner := NoVNCRunner{
		Supervisor: supervisor,
		LoadConfig: func() (NoVNCLaunchConfig, error) {
			return NoVNCLaunchConfig{
				BrowserCommand:      commandPath,
				DisplayCommand:      commandPath,
				WebsockifyCommand:   commandPath,
				AllowedCommandPaths: []string{commandPath},
				BindAddr:            "127.0.0.1:6080",
				PrivateBaseURL:      "https://odin-handoff.tailnet.local",
				TimeoutSeconds:      300,
			}, nil
		},
	}

	response, err := runner.Start(context.Background(), validFixtureStartRequest())
	if err != nil {
		t.Fatalf("NoVNCRunner.Start() error = %v", err)
	}
	if response.Status != StatusCompleted {
		t.Fatalf("response.Status = %q, want %q", response.Status, StatusCompleted)
	}
	if response.RunnerID != "novnc-1001-1002-1003" || response.ProcessID != 1003 {
		t.Fatalf("response = %+v, want aggregate runner/process metadata", response)
	}
	if response.ViewerURL != "https://odin-handoff.tailnet.local/session/novnc-1001-1002-1003" {
		t.Fatalf("response.ViewerURL = %q, want private runner viewer URL", response.ViewerURL)
	}
	if response.BindAddr != "127.0.0.1:6080" || response.PrivateBaseURL != "https://odin-handoff.tailnet.local" {
		t.Fatalf("response = %+v, want validated bind/private base metadata", response)
	}
	if got := processRoles(supervisor.started); strings.Join(got, ",") != "display,browser,novnc" {
		t.Fatalf("started roles = %v, want display,browser,novnc", got)
	}
	if len(response.ChildProcesses) != 3 {
		t.Fatalf("ChildProcesses = %+v, want three process results", response.ChildProcesses)
	}
	if runner.LaunchCount() != 0 {
		t.Fatalf("LaunchCount() = %d, want 0", runner.LaunchCount())
	}
}

func TestNoVNCRunnerStartReturnsFailedWhenAProcessFails(t *testing.T) {
	commandPath := testExecutablePath(t, "true")
	supervisor := &fakeNoVNCProcessSupervisor{
		results: map[string]ProcessStatus{
			"display": ProcessStatusExited,
			"browser": ProcessStatusFailed,
			"novnc":   ProcessStatusExited,
		},
	}
	runner := NoVNCRunner{
		Supervisor: supervisor,
		LoadConfig: func() (NoVNCLaunchConfig, error) {
			return validNoVNCLaunchConfig(commandPath), nil
		},
	}

	response, err := runner.Start(context.Background(), validFixtureStartRequest())
	if err != nil {
		t.Fatalf("NoVNCRunner.Start() error = %v", err)
	}
	if response.Status != StatusFailed || response.ErrorCode != "novnc_process_failed" {
		t.Fatalf("response = %+v, want failed novnc_process_failed", response)
	}
	if response.RunnerID == "" || response.ProcessID == 0 || response.ViewerURL == "" {
		t.Fatalf("response = %+v, want started metadata preserved on failure", response)
	}
	if len(supervisor.cancelled) == 0 {
		t.Fatalf("cancelled handles = %v, want cleanup after process failure", supervisor.cancelled)
	}
}

func TestNoVNCRunnerStartReturnsExpiredOnTimeout(t *testing.T) {
	commandPath := testExecutablePath(t, "true")
	supervisor := &fakeNoVNCProcessSupervisor{
		results: map[string]ProcessStatus{
			"display": ProcessStatusTimeout,
			"browser": ProcessStatusExited,
			"novnc":   ProcessStatusExited,
		},
	}
	runner := NoVNCRunner{
		Supervisor: supervisor,
		LoadConfig: func() (NoVNCLaunchConfig, error) {
			return validNoVNCLaunchConfig(commandPath), nil
		},
	}

	response, err := runner.Start(context.Background(), validFixtureStartRequest())
	if err != nil {
		t.Fatalf("NoVNCRunner.Start(timeout) error = %v", err)
	}
	if response.Status != StatusExpired || response.ErrorCode != "novnc_timeout" {
		t.Fatalf("response = %+v, want expired novnc_timeout", response)
	}
	if response.ViewerURL != "https://odin-handoff.tailnet.local/session/novnc-1001-1002-1003" {
		t.Fatalf("response.ViewerURL = %q, want private runner viewer URL retained", response.ViewerURL)
	}
}

func TestNoVNCRunnerStartRejectsInvalidLaunchConfig(t *testing.T) {
	commandPath := testExecutablePath(t, "true")
	supervisor := &fakeNoVNCProcessSupervisor{}
	runner := NoVNCRunner{
		Supervisor: supervisor,
		LoadConfig: func() (NoVNCLaunchConfig, error) {
			return NoVNCLaunchConfig{
				BrowserCommand:      commandPath,
				DisplayCommand:      commandPath,
				WebsockifyCommand:   commandPath,
				AllowedCommandPaths: []string{"/usr/bin/not-allowed"},
				BindAddr:            "127.0.0.1:6080",
				PrivateBaseURL:      "https://odin-handoff.tailnet.local",
				TimeoutSeconds:      300,
			}, nil
		},
	}

	_, err := runner.Start(context.Background(), validFixtureStartRequest())
	if err == nil || !strings.Contains(err.Error(), "allowlist") {
		t.Fatalf("NoVNCRunner.Start() error = %v, want allowlist rejection", err)
	}
	if len(supervisor.started) != 0 {
		t.Fatalf("started processes = %+v, want no launch after config rejection", supervisor.started)
	}
}

func TestNoVNCRunnerStartRejectsNonFixtureSafeCommands(t *testing.T) {
	realBrowserPath := "/opt/google/chrome/chrome"
	supervisor := &fakeNoVNCProcessSupervisor{}
	runner := NoVNCRunner{
		Supervisor: supervisor,
		LoadConfig: func() (NoVNCLaunchConfig, error) {
			return NoVNCLaunchConfig{
				BrowserCommand:      realBrowserPath,
				DisplayCommand:      testExecutablePath(t, "true"),
				WebsockifyCommand:   testExecutablePath(t, "true"),
				AllowedCommandPaths: []string{realBrowserPath, testExecutablePath(t, "true")},
				BindAddr:            "127.0.0.1:6080",
				PrivateBaseURL:      "https://odin-handoff.tailnet.local",
				TimeoutSeconds:      300,
			}, nil
		},
	}

	_, err := runner.Start(context.Background(), validFixtureStartRequest())
	if err == nil || !strings.Contains(err.Error(), "fixture-safe") {
		t.Fatalf("NoVNCRunner.Start(real browser) error = %v, want fixture-safe rejection", err)
	}
	if len(supervisor.started) != 0 {
		t.Fatalf("started processes = %+v, want no launch for non-fixture command", supervisor.started)
	}
}

func TestNoVNCRunnerCancelReturnsNotImplemented(t *testing.T) {
	response, err := NoVNCRunner{}.Cancel(context.Background(), CancelRequest{
		RunnerID: "novnc-1",
		Reason:   "operator cancelled",
	})
	if err != nil {
		t.Fatalf("NoVNCRunner.Cancel() error = %v", err)
	}
	if response.RunnerID != "novnc-1" || response.Status != StatusNotImplemented || response.ErrorCode != "not_implemented" {
		t.Fatalf("Cancel() = %+v, want structured not_implemented response", response)
	}
}

func TestFixtureRunnerRequiresExplicitEnablementAndAllowlist(t *testing.T) {
	valid := validFixtureStartRequest()
	commandPath := testExecutablePath(t, "true")

	_, err := FixtureRunner{
		Command:         commandPath,
		AllowedCommands: []string{commandPath},
	}.Start(context.Background(), valid)
	if err == nil || !strings.Contains(err.Error(), "explicitly enabled") {
		t.Fatalf("FixtureRunner disabled Start() error = %v, want explicit enablement rejection", err)
	}

	_, err = FixtureRunner{
		Enabled:         true,
		Command:         commandPath,
		AllowedCommands: []string{"/not/allowed"},
	}.Start(context.Background(), valid)
	if err == nil || !strings.Contains(err.Error(), "allowlist") {
		t.Fatalf("FixtureRunner disallowed Start() error = %v, want allowlist rejection", err)
	}
}

func TestFixtureRunnerStartReturnsStartedForAllowedCommand(t *testing.T) {
	commandPath := testExecutablePath(t, "true")
	response, err := FixtureRunner{
		Enabled:         true,
		Command:         commandPath,
		AllowedCommands: []string{commandPath},
		TimeoutSeconds:  2,
	}.Start(context.Background(), validFixtureStartRequest())
	if err != nil {
		t.Fatalf("FixtureRunner.Start() error = %v", err)
	}
	if response.Status != StatusStarted {
		t.Fatalf("response.Status = %q, want %q", response.Status, StatusStarted)
	}
	if response.RunnerID == "" || response.ProcessID <= 0 {
		t.Fatalf("response = %+v, want runner_id and process_id", response)
	}
	if response.ViewerURL != "" {
		t.Fatalf("response.ViewerURL = %q, want empty fixture viewer URL", response.ViewerURL)
	}
}

func TestFixtureRunnerStartReturnsExpiredOnTimeout(t *testing.T) {
	commandPath := testExecutablePath(t, "sleep")
	response, err := FixtureRunner{
		Enabled:         true,
		Command:         commandPath,
		Args:            []string{"2"},
		AllowedCommands: []string{commandPath},
		TimeoutSeconds:  1,
	}.Start(context.Background(), validFixtureStartRequest())
	if err != nil {
		t.Fatalf("FixtureRunner.Start(timeout) error = %v", err)
	}
	if response.Status != StatusExpired || response.ErrorCode != "fixture_timeout" {
		t.Fatalf("response = %+v, want expired fixture timeout", response)
	}
	if response.ProcessID <= 0 {
		t.Fatalf("response.ProcessID = %d, want captured process id", response.ProcessID)
	}
}

func TestNoVNCPlanRejectsInvalidCommandPath(t *testing.T) {
	config := validNoVNCConfig(t)
	config.BrowserCommand = "chromium"

	_, err := PlanNoVNCStart(validFixtureStartRequest(), config)
	if err == nil || !strings.Contains(err.Error(), "browser command") {
		t.Fatalf("PlanNoVNCStart(relative browser command) error = %v, want browser command rejection", err)
	}
}

func TestNoVNCPlanRejectsMissingDisplayCommand(t *testing.T) {
	config := validNoVNCConfig(t)
	config.DisplayCommand = ""

	_, err := PlanNoVNCStart(validFixtureStartRequest(), config)
	if err == nil || !strings.Contains(err.Error(), "display command") || !strings.Contains(err.Error(), "required") {
		t.Fatalf("PlanNoVNCStart(missing display command) error = %v, want display command required rejection", err)
	}
}

func TestNoVNCPlanRejectsNonAllowlistedCommandPath(t *testing.T) {
	config := validNoVNCConfig(t)
	config.NoVNCAllowedCommands = []string{"/usr/bin/not-allowed"}

	_, err := PlanNoVNCStart(validFixtureStartRequest(), config)
	if err == nil || !strings.Contains(err.Error(), "novnc command") || !strings.Contains(err.Error(), "allowlist") {
		t.Fatalf("PlanNoVNCStart(disallowed novnc command) error = %v, want allowlist rejection", err)
	}
}

func TestNoVNCPlanRejectsNonAllowlistedBrowserCommandPath(t *testing.T) {
	config := validNoVNCConfig(t)
	config.BrowserAllowedCommands = []string{"/usr/bin/not-allowed"}

	_, err := PlanNoVNCStart(validFixtureStartRequest(), config)
	if err == nil || !strings.Contains(err.Error(), "browser command") || !strings.Contains(err.Error(), "allowlist") {
		t.Fatalf("PlanNoVNCStart(disallowed browser command) error = %v, want browser allowlist rejection", err)
	}
}

func TestDetectNoVNCWebsockifyCommandAcceptsExplicitExecutablePath(t *testing.T) {
	commandPath := testExecutablePath(t, "true")

	detection, err := DetectNoVNCWebsockifyCommand(commandPath, []string{commandPath})
	if err != nil {
		t.Fatalf("DetectNoVNCWebsockifyCommand() error = %v", err)
	}
	if detection.DetectedPath != commandPath {
		t.Fatalf("DetectedPath = %q, want %q", detection.DetectedPath, commandPath)
	}
	if detection.CommandRole != NoVNCWebsockifyCommandRole {
		t.Fatalf("CommandRole = %q, want %q", detection.CommandRole, NoVNCWebsockifyCommandRole)
	}
	if detection.ValidationStatus != NoVNCCommandValidationValid {
		t.Fatalf("ValidationStatus = %q, want %q", detection.ValidationStatus, NoVNCCommandValidationValid)
	}
	if detection.ErrorCode != "" || detection.ErrorMessage != "" {
		t.Fatalf("detection = %+v, want no error metadata", detection)
	}
}

func TestDetectNoVNCCommandAcceptsDisplayAndBrowserRoles(t *testing.T) {
	commandPath := testExecutablePath(t, "true")
	tests := []struct {
		name string
		role string
	}{
		{name: "display", role: NoVNCDisplayCommandRole},
		{name: "browser", role: NoVNCBrowserCommandRole},
		{name: "novnc", role: NoVNCWebsockifyCommandRole},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			detection, err := DetectNoVNCCommand(test.name+" command", test.role, commandPath, []string{commandPath})
			if err != nil {
				t.Fatalf("DetectNoVNCCommand(%s) error = %v", test.name, err)
			}
			if detection.DetectedPath != commandPath {
				t.Fatalf("DetectedPath = %q, want %q", detection.DetectedPath, commandPath)
			}
			if detection.CommandRole != test.role {
				t.Fatalf("CommandRole = %q, want %q", detection.CommandRole, test.role)
			}
			if detection.ValidationStatus != NoVNCCommandValidationValid {
				t.Fatalf("ValidationStatus = %q, want %q", detection.ValidationStatus, NoVNCCommandValidationValid)
			}
		})
	}
}

func TestDetectNoVNCWebsockifyCommandRejectsUnsafeValues(t *testing.T) {
	commandPath := testExecutablePath(t, "true")
	nonExecutablePath := filepath.Join(t.TempDir(), "websockify")
	if err := os.WriteFile(nonExecutablePath, []byte("#!/bin/sh\nexit 0\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(non-executable fixture) error = %v", err)
	}
	missingPath := filepath.Join(t.TempDir(), "missing-websockify")

	tests := []struct {
		name    string
		command string
		allowed []string
		want    string
	}{
		{name: "missing path", command: "", allowed: []string{commandPath}, want: NoVNCCommandErrorMissing},
		{name: "relative path", command: "websockify", allowed: []string{"websockify"}, want: NoVNCCommandErrorRelative},
		{name: "not allowlisted", command: commandPath, allowed: []string{"/usr/bin/not-allowed"}, want: NoVNCCommandErrorNotAllowlisted},
		{name: "not found", command: missingPath, allowed: []string{missingPath}, want: NoVNCCommandErrorNotFound},
		{name: "not executable", command: nonExecutablePath, allowed: []string{nonExecutablePath}, want: NoVNCCommandErrorNotExecutable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			detection, err := DetectNoVNCWebsockifyCommand(test.command, test.allowed)
			if err == nil {
				t.Fatalf("DetectNoVNCWebsockifyCommand() error = nil, want rejection")
			}
			if detection.CommandRole != NoVNCWebsockifyCommandRole {
				t.Fatalf("CommandRole = %q, want %q", detection.CommandRole, NoVNCWebsockifyCommandRole)
			}
			if detection.ValidationStatus != NoVNCCommandValidationInvalid {
				t.Fatalf("ValidationStatus = %q, want %q", detection.ValidationStatus, NoVNCCommandValidationInvalid)
			}
			if detection.ErrorCode != test.want {
				t.Fatalf("ErrorCode = %q, want %q; detection = %+v", detection.ErrorCode, test.want, detection)
			}
			if strings.TrimSpace(detection.ErrorMessage) == "" {
				t.Fatalf("ErrorMessage is empty for detection %+v", detection)
			}
		})
	}
}

func TestNoVNCPlanRejectsPublicBindAddress(t *testing.T) {
	config := validNoVNCConfig(t)
	config.BindAddr = "0.0.0.0:6080"

	_, err := PlanNoVNCStart(validFixtureStartRequest(), config)
	if err == nil || !strings.Contains(err.Error(), "bind_addr") {
		t.Fatalf("PlanNoVNCStart(public bind) error = %v, want bind_addr rejection", err)
	}
}

func TestNoVNCPlanRequiresPrivateBaseURLAndBoundedTimeout(t *testing.T) {
	t.Run("private base url", func(t *testing.T) {
		config := validNoVNCConfig(t)
		config.PrivateBaseURL = ""

		_, err := PlanNoVNCStart(validFixtureStartRequest(), config)
		if err == nil || !strings.Contains(err.Error(), "private_base_url") {
			t.Fatalf("PlanNoVNCStart(missing private base URL) error = %v, want private_base_url rejection", err)
		}
	})

	t.Run("timeout bound", func(t *testing.T) {
		config := validNoVNCConfig(t)
		config.TimeoutSeconds = 601

		_, err := PlanNoVNCStart(validFixtureStartRequest(), config)
		if err == nil || !strings.Contains(err.Error(), "timeout_seconds") {
			t.Fatalf("PlanNoVNCStart(timeout beyond request) error = %v, want timeout_seconds rejection", err)
		}
	})
}

func TestNoVNCPlanGeneratesDryRunPlanWithoutLaunching(t *testing.T) {
	request := validFixtureStartRequest()
	request.HandoffID = "opaque handoff"
	config := validNoVNCConfig(t)

	plan, err := PlanNoVNCStart(request, config)
	if err != nil {
		t.Fatalf("PlanNoVNCStart() error = %v", err)
	}
	if plan.BindAddr != config.BindAddr || plan.PrivateBaseURL != config.PrivateBaseURL || plan.TimeoutSeconds != config.TimeoutSeconds {
		t.Fatalf("plan config = %+v, want bind/base/timeout from config %+v", plan, config)
	}
	if plan.ViewerURL != "https://odin-handoff.tailnet.local/session/dry-run-opaque%20handoff" {
		t.Fatalf("plan.ViewerURL = %q, want private dry-run viewer URL", plan.ViewerURL)
	}
	if len(plan.Commands) != 3 {
		t.Fatalf("plan.Commands = %+v, want display, browser, and novnc commands", plan.Commands)
	}
	for _, wantRole := range []string{"display", "browser", "novnc"} {
		found := false
		for _, command := range plan.Commands {
			if command.Role == wantRole {
				found = true
				if command.Path == "" {
					t.Fatalf("command %+v has empty path", command)
				}
			}
		}
		if !found {
			t.Fatalf("plan.Commands = %+v, missing role %q", plan.Commands, wantRole)
		}
	}
}

func TestNoVNCPlanIncludesCommandDetectionMetadata(t *testing.T) {
	config := validNoVNCConfig(t)

	plan, err := PlanNoVNCStart(validFixtureStartRequest(), config)
	if err != nil {
		t.Fatalf("PlanNoVNCStart() error = %v", err)
	}

	wants := map[string]struct {
		path string
		role string
	}{
		"display": {path: config.DisplayCommand, role: NoVNCDisplayCommandRole},
		"browser": {path: config.BrowserCommand, role: NoVNCBrowserCommandRole},
		"novnc":   {path: config.NoVNCCommand, role: NoVNCWebsockifyCommandRole},
	}
	for _, command := range plan.Commands {
		want, ok := wants[command.Role]
		if !ok {
			t.Fatalf("unexpected command role %q in plan %+v", command.Role, plan.Commands)
		}
		if command.DetectedPath != want.path {
			t.Fatalf("%s DetectedPath = %q, want %q", command.Role, command.DetectedPath, want.path)
		}
		if command.CommandRole != want.role {
			t.Fatalf("%s CommandRole = %q, want %q", command.Role, command.CommandRole, want.role)
		}
		if command.ValidationStatus != NoVNCCommandValidationValid {
			t.Fatalf("%s ValidationStatus = %q, want %q", command.Role, command.ValidationStatus, NoVNCCommandValidationValid)
		}
		if command.ErrorCode != "" || command.ErrorMessage != "" {
			t.Fatalf("%s command = %+v, want no detection error metadata", command.Role, command)
		}
		delete(wants, command.Role)
	}
	if len(wants) != 0 {
		t.Fatalf("plan.Commands = %+v, missing detection metadata for %v", plan.Commands, wants)
	}
}

func TestNoVNCLaunchConfigFromEnvValidatesCommonAllowlist(t *testing.T) {
	commandPath := testExecutablePath(t, "true")
	t.Setenv(NoVNCBrowserCommandEnvVar, commandPath)
	t.Setenv(NoVNCDisplayCommandEnvVar, commandPath)
	t.Setenv(NoVNCWebsockifyCommandEnvVar, commandPath)
	t.Setenv(NoVNCAllowedCommandsEnvVar, strings.Join([]string{"/usr/bin/not-used", commandPath}, ","))
	t.Setenv(NoVNCBindAddrEnvVar, "127.0.0.1:6080")
	t.Setenv(NoVNCPrivateBaseURLEnvVar, "https://odin-handoff.tailnet.local/")
	t.Setenv(NoVNCTimeoutSecondsEnvVar, "300")

	config, err := LoadNoVNCLaunchConfigFromEnv()
	if err != nil {
		t.Fatalf("LoadNoVNCLaunchConfigFromEnv() error = %v", err)
	}
	normalized, err := ValidateNoVNCLaunchConfig(config, 600)
	if err != nil {
		t.Fatalf("ValidateNoVNCLaunchConfig() error = %v", err)
	}
	if normalized.BrowserCommand != commandPath || normalized.DisplayCommand != commandPath || normalized.WebsockifyCommand != commandPath {
		t.Fatalf("normalized commands = %+v, want env command path", normalized)
	}
	if normalized.BindAddr != "127.0.0.1:6080" || normalized.PrivateBaseURL != "https://odin-handoff.tailnet.local" || normalized.TimeoutSeconds != 300 {
		t.Fatalf("normalized config = %+v, want validated bind/base/timeout", normalized)
	}
}

func TestNoVNCLaunchConfigRejectsUnsafeValues(t *testing.T) {
	commandPath := testExecutablePath(t, "true")
	tests := []struct {
		name    string
		mutate  func(*NoVNCLaunchConfig)
		wantErr string
	}{
		{name: "relative browser command", mutate: func(config *NoVNCLaunchConfig) { config.BrowserCommand = "chromium" }, wantErr: "browser command"},
		{name: "not allowlisted websockify", mutate: func(config *NoVNCLaunchConfig) { config.AllowedCommandPaths = []string{"/usr/bin/not-allowed"} }, wantErr: "allowlist"},
		{name: "public bind", mutate: func(config *NoVNCLaunchConfig) { config.BindAddr = "0.0.0.0:6080" }, wantErr: "bind_addr"},
		{name: "missing private base url", mutate: func(config *NoVNCLaunchConfig) { config.PrivateBaseURL = "" }, wantErr: "private_base_url"},
		{name: "timeout beyond request", mutate: func(config *NoVNCLaunchConfig) { config.TimeoutSeconds = 601 }, wantErr: "timeout_seconds"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := NoVNCLaunchConfig{
				BrowserCommand:      commandPath,
				DisplayCommand:      commandPath,
				WebsockifyCommand:   commandPath,
				AllowedCommandPaths: []string{commandPath},
				BindAddr:            "127.0.0.1:6080",
				PrivateBaseURL:      "https://odin-handoff.tailnet.local",
				TimeoutSeconds:      300,
			}
			test.mutate(&config)
			if _, err := ValidateNoVNCLaunchConfig(config, 600); err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("ValidateNoVNCLaunchConfig() error = %v, want substring %q", err, test.wantErr)
			}
		})
	}
}

func TestNoVNCLaunchConfigDefaultsBindAddrToLoopback(t *testing.T) {
	commandPath := testExecutablePath(t, "true")
	config := NoVNCLaunchConfig{
		BrowserCommand:      commandPath,
		DisplayCommand:      commandPath,
		WebsockifyCommand:   commandPath,
		AllowedCommandPaths: []string{commandPath},
		PrivateBaseURL:      "https://odin-handoff.tailnet.local",
		TimeoutSeconds:      300,
	}

	normalized, err := ValidateNoVNCLaunchConfig(config, 600)
	if err != nil {
		t.Fatalf("ValidateNoVNCLaunchConfig(default bind) error = %v", err)
	}
	if normalized.BindAddr != "127.0.0.1:0" {
		t.Fatalf("normalized.BindAddr = %q, want loopback default", normalized.BindAddr)
	}
}

func TestNoVNCDryRunPlannerSourceDoesNotImportExec(t *testing.T) {
	payload, err := os.ReadFile("novnc.go")
	if err != nil {
		t.Fatalf("ReadFile(novnc.go) error = %v", err)
	}
	source := string(payload)
	for _, forbidden := range []string{"os/" + "exec", "exec." + "Command"} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("novnc.go contains forbidden process launch token %q", forbidden)
		}
	}
}

func validFixtureStartRequest() StartRequest {
	return StartRequest{
		SessionID:      1,
		LoginRequestID: 2,
		HandoffID:      "opaque-handoff-id",
		ProfilePath:    "browser-sessions/profiles/marcus-example",
		AllowedDomain:  "example.com",
		TimeoutSeconds: 600,
	}
}

func validNoVNCConfig(t *testing.T) NoVNCRunnerConfig {
	t.Helper()
	commandPath := testExecutablePath(t, "true")
	return NoVNCRunnerConfig{
		BrowserCommand:         commandPath,
		BrowserAllowedCommands: []string{commandPath},
		DisplayCommand:         commandPath,
		DisplayAllowedCommands: []string{commandPath},
		NoVNCCommand:           commandPath,
		NoVNCAllowedCommands:   []string{commandPath},
		BindAddr:               "127.0.0.1:6080",
		PrivateBaseURL:         "https://odin-handoff.tailnet.local",
		TimeoutSeconds:         300,
	}
}

func validNoVNCLaunchConfig(commandPath string) NoVNCLaunchConfig {
	return NoVNCLaunchConfig{
		BrowserCommand:      commandPath,
		DisplayCommand:      commandPath,
		WebsockifyCommand:   commandPath,
		AllowedCommandPaths: []string{commandPath},
		BindAddr:            "127.0.0.1:6080",
		PrivateBaseURL:      "https://odin-handoff.tailnet.local",
		TimeoutSeconds:      300,
	}
}

func testExecutablePath(t *testing.T, name string) string {
	t.Helper()
	for _, dir := range []string{"/usr/bin", "/bin"} {
		path := filepath.Join(dir, name)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path
		}
	}
	t.Fatalf("required fixture executable %q not found", name)
	return ""
}

type fakeNoVNCProcessSupervisor struct {
	started   []StartProcessRequest
	waited    []ProcessHandle
	cancelled []ProcessHandle
	results   map[string]ProcessStatus
	nextPID   int64
}

func (supervisor *fakeNoVNCProcessSupervisor) StartProcess(_ context.Context, request StartProcessRequest) (ProcessHandle, error) {
	request, err := validateStartProcessRequest(request)
	if err != nil {
		return ProcessHandle{}, err
	}
	supervisor.started = append(supervisor.started, request)
	if supervisor.nextPID == 0 {
		supervisor.nextPID = 1000
	}
	supervisor.nextPID++
	return ProcessHandle{
		PID:         supervisor.nextPID,
		Role:        request.Role,
		CommandPath: request.CommandPath,
		StartedAt:   time.Unix(supervisor.nextPID, 0).UTC(),
		Status:      ProcessStatusStarted,
	}, nil
}

func (supervisor *fakeNoVNCProcessSupervisor) WaitProcess(_ context.Context, handle ProcessHandle) (ProcessResult, error) {
	supervisor.waited = append(supervisor.waited, handle)
	status := supervisor.results[handle.Role]
	if status == "" {
		status = ProcessStatusExited
	}
	exitedAt := handle.StartedAt.Add(time.Second)
	result := ProcessResult{
		PID:         handle.PID,
		Role:        handle.Role,
		CommandPath: handle.CommandPath,
		StartedAt:   handle.StartedAt,
		ExitedAt:    &exitedAt,
		Status:      status,
	}
	switch status {
	case ProcessStatusFailed:
		result.ErrorMessage = handle.Role + " failed"
	case ProcessStatusTimeout:
		result.ErrorMessage = handle.Role + " timed out"
	}
	return result, nil
}

func (supervisor *fakeNoVNCProcessSupervisor) CancelProcess(_ context.Context, handle ProcessHandle, reason string) (ProcessResult, error) {
	supervisor.cancelled = append(supervisor.cancelled, handle)
	exitedAt := handle.StartedAt.Add(time.Second)
	return ProcessResult{
		PID:          handle.PID,
		Role:         handle.Role,
		CommandPath:  handle.CommandPath,
		StartedAt:    handle.StartedAt,
		ExitedAt:     &exitedAt,
		Status:       ProcessStatusCancelled,
		ErrorMessage: strings.TrimSpace(reason),
	}, nil
}

func processRoles(requests []StartProcessRequest) []string {
	roles := make([]string, 0, len(requests))
	for _, request := range requests {
		roles = append(roles, request.Role)
	}
	return roles
}
