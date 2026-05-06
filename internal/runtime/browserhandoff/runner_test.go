package browserhandoff

import (
	"context"
	"strings"
	"testing"
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
