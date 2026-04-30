package github

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"odin-os/internal/tracker"
)

func TestNewClientReturnsReadOnlyPlaceholder(t *testing.T) {
	t.Parallel()

	client := NewClient()
	if client == nil {
		t.Fatal("NewClient() = nil, want placeholder client")
	}
}

func TestListEligibleIssuesCurrentlyReturnsNotImplemented(t *testing.T) {
	t.Parallel()

	issues, err := NewClient().ListEligibleIssues(context.Background())
	if !errors.Is(err, tracker.ErrNotImplemented) {
		t.Fatalf("ListEligibleIssues() error = %v, want %v", err, tracker.ErrNotImplemented)
	}
	if issues != nil {
		t.Fatalf("ListEligibleIssues() issues = %#v, want nil", issues)
	}
}

func TestClientImplementsTrackerInterface(t *testing.T) {
	t.Parallel()

	var _ tracker.Tracker = NewClient()
}

func TestFetchEligibleIssuesFiltersReadyOpenIssuesAndSkipsBlockedAndPullRequests(t *testing.T) {
	t.Parallel()
	skipIfLoopbackListenUnavailable(t)

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", request.Method)
		}
		if request.URL.Path != "/repos/acme/widgets/issues" {
			t.Fatalf("path = %s, want /repos/acme/widgets/issues", request.URL.Path)
		}
		if got := request.URL.Query().Get("state"); got != "open" {
			t.Fatalf("state query = %q, want open", got)
		}
		fmt.Fprint(response, `[
			{"number":1,"title":"ready","body":"build it","html_url":"https://github.example/acme/widgets/issues/1","state":"open","labels":[{"name":"odin:ready"},{"name":"backend"}]},
			{"number":2,"title":"blocked","state":"open","labels":[{"name":"odin:ready"},{"name":"odin:blocked"}]},
			{"number":3,"title":"missing ready","state":"open","labels":[{"name":"backend"}]},
			{"number":4,"title":"pull request","state":"open","pull_request":{},"labels":[{"name":"odin:ready"}]}
		]`)
	}))
	defer server.Close()

	client := NewClientWithConfig(Config{
		BaseURL:        server.URL,
		Owner:          "acme",
		Repo:           "widgets",
		EligibleLabels: []string{"odin:ready"},
		BlockedLabels:  []string{"odin:blocked"},
	})

	issues, err := client.FetchEligibleIssues(context.Background())
	if err != nil {
		t.Fatalf("FetchEligibleIssues() error = %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("FetchEligibleIssues() len = %d, want 1: %#v", len(issues), issues)
	}
	if issues[0].Number != 1 || issues[0].Repo != "acme/widgets" || issues[0].Provider != "github" {
		t.Fatalf("eligible issue = %#v, want github acme/widgets #1", issues[0])
	}
	if !containsLabel(issues[0].Labels, "backend") {
		t.Fatalf("eligible issue labels = %#v, want backend preserved", issues[0].Labels)
	}
	audit := client.RequestAudit()
	if audit.Reads != 1 || audit.Writes != 0 || len(audit.Forbidden) != 0 {
		t.Fatalf("audit = %+v, want reads=1 writes=0 forbidden=0", audit)
	}
}

func TestRequestAuditCountsForbiddenGitHubWriteMethodsWithoutTokenValues(t *testing.T) {
	t.Parallel()
	skipIfLoopbackListenUnavailable(t)

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.WriteHeader(http.StatusOK)
		fmt.Fprint(response, `{}`)
	}))
	defer server.Close()

	client := NewClientWithConfig(Config{
		BaseURL: server.URL,
		Owner:   "acme",
		Repo:    "widgets",
		Token:   "ghp_1234567890abcdefghijklmnopqrstuvwx",
	})

	if err := client.AddComment(context.Background(), tracker.IssueID{Provider: "github", Repo: "acme/widgets", Number: 7}, "comment"); err != nil {
		t.Fatalf("AddComment() error = %v", err)
	}

	audit := client.RequestAudit()
	if audit.Reads != 0 || audit.Writes != 1 || len(audit.Forbidden) != 1 {
		t.Fatalf("audit = %+v, want reads=0 writes=1 forbidden=1", audit)
	}
	if audit.Forbidden[0].Method != http.MethodPost || audit.Forbidden[0].Path != "/repos/acme/widgets/issues/7/comments" {
		t.Fatalf("forbidden = %+v, want POST comments path", audit.Forbidden[0])
	}
	if strings.Contains(fmt.Sprintf("%+v", audit), "ghp_1234567890abcdefghijklmnopqrstuvwx") {
		t.Fatalf("audit leaked token: %+v", audit)
	}
}

func TestDryRunTrackerMutationsDoNotWriteToGitHub(t *testing.T) {
	t.Parallel()
	skipIfLoopbackListenUnavailable(t)

	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests++
		t.Fatalf("dry-run unexpectedly called GitHub: %s %s", request.Method, request.URL.Path)
	}))
	defer server.Close()

	client := NewClientWithConfig(Config{
		BaseURL: server.URL,
		Owner:   "acme",
		Repo:    "widgets",
		DryRun:  true,
	})
	id := tracker.IssueID{Provider: "github", Repo: "acme/widgets", Number: 7}

	if err := client.MarkInProgress(context.Background(), id); err != nil {
		t.Fatalf("MarkInProgress() error = %v", err)
	}
	if err := client.AddComment(context.Background(), id, "dry run comment"); err != nil {
		t.Fatalf("AddComment() error = %v", err)
	}
	issue, err := client.CreateFollowUpIssue(context.Background(), tracker.FollowUpIssue{
		Repo:   "acme/widgets",
		Title:  "Follow up",
		Body:   "details",
		Labels: []string{"odin:ready"},
	})
	if err != nil {
		t.Fatalf("CreateFollowUpIssue() error = %v", err)
	}
	if requests != 0 {
		t.Fatalf("dry-run requests = %d, want 0", requests)
	}
	if issue.Provider != "github" || issue.Repo != "acme/widgets" || issue.Title != "Follow up" {
		t.Fatalf("dry-run follow-up issue = %#v, want projected issue", issue)
	}
}

func TestGitHubErrorsRedactTokenLikeStrings(t *testing.T) {
	t.Parallel()
	skipIfLoopbackListenUnavailable(t)

	const token = "ghp_1234567890abcdefghijklmnopqrstuvwx"
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(response, `{"message":"token %s failed"}`, token)
	}))
	defer server.Close()

	client := NewClientWithConfig(Config{
		BaseURL: server.URL,
		Owner:   "acme",
		Repo:    "widgets",
		Token:   token,
	})

	_, err := client.FetchEligibleIssues(context.Background())
	if err == nil {
		t.Fatal("FetchEligibleIssues() error = nil, want redacted GitHub error")
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("error = %q, want token redacted", err.Error())
	}
	if !strings.Contains(err.Error(), "[REDACTED]") {
		t.Fatalf("error = %q, want redaction marker", err.Error())
	}
}

func TestLifecycleMarkersUseCanonicalOdinLabelsAndIssueEndpoints(t *testing.T) {
	t.Parallel()
	skipIfLoopbackListenUnavailable(t)

	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatalf("ReadAll() error = %v", err)
		}
		requests = append(requests, request.Method+" "+request.URL.Path+" "+string(body))
		response.WriteHeader(http.StatusOK)
		fmt.Fprint(response, `{}`)
	}))
	defer server.Close()

	client := NewClientWithConfig(Config{
		BaseURL: server.URL,
		Owner:   "acme",
		Repo:    "widgets",
	})
	id := tracker.IssueID{Provider: "github", Repo: "acme/widgets", Number: 7}

	for name, run := range map[string]func(context.Context, tracker.IssueID) error{
		"in_progress": client.MarkInProgress,
		"blocked": func(ctx context.Context, id tracker.IssueID) error {
			return client.MarkBlocked(ctx, id, "blocked by policy")
		},
		"failed":           func(ctx context.Context, id tracker.IssueID) error { return client.MarkFailed(ctx, id, "tests failed") },
		"ready_for_review": client.MarkReadyForReview,
		"done":             client.MarkDone,
	} {
		if err := run(context.Background(), id); err != nil {
			t.Fatalf("%s marker error = %v", name, err)
		}
	}

	wantFragments := []string{
		`POST /repos/acme/widgets/issues/7/labels {"labels":["odin:running"]}`,
		`POST /repos/acme/widgets/issues/7/labels {"labels":["odin:blocked"]}`,
		`POST /repos/acme/widgets/issues/7/comments {"body":"blocked by policy"}`,
		`POST /repos/acme/widgets/issues/7/labels {"labels":["odin:failed"]}`,
		`POST /repos/acme/widgets/issues/7/comments {"body":"tests failed"}`,
		`POST /repos/acme/widgets/issues/7/labels {"labels":["odin:human-review"]}`,
		`POST /repos/acme/widgets/issues/7/labels {"labels":["odin:done"]}`,
		`PATCH /repos/acme/widgets/issues/7 {"state":"closed"}`,
	}
	for _, want := range wantFragments {
		if !containsString(requests, want) {
			t.Fatalf("requests = %#v, want fragment %q", requests, want)
		}
	}
}

func skipIfLoopbackListenUnavailable(t *testing.T) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("loopback listener unavailable in this environment: %v", err)
		}
		t.Fatalf("loopback listener preflight failed: %v", err)
	}
	if err := listener.Close(); err != nil {
		t.Fatalf("close loopback listener: %v", err)
	}
}

func containsLabel(labels []string, want string) bool {
	for _, label := range labels {
		if label == want {
			return true
		}
	}
	return false
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
