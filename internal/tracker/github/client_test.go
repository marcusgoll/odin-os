package github

import (
	"context"
	"errors"
	"fmt"
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
}

func TestTrackerMutationsFailClosedWithoutCallingGitHub(t *testing.T) {
	t.Parallel()

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
	})
	id := tracker.IssueID{Provider: "github", Repo: "acme/widgets", Number: 7}

	for name, run := range map[string]func() error{
		"MarkInProgress": func() error { return client.MarkInProgress(context.Background(), id) },
		"MarkBlocked":    func() error { return client.MarkBlocked(context.Background(), id, "blocked by policy") },
		"MarkFailed":     func() error { return client.MarkFailed(context.Background(), id, "tests failed") },
		"MarkReadyForReview": func() error {
			return client.MarkReadyForReview(context.Background(), id)
		},
		"MarkDone":   func() error { return client.MarkDone(context.Background(), id) },
		"AddComment": func() error { return client.AddComment(context.Background(), id, "comment") },
		"CreateFollowUpIssue": func() error {
			_, err := client.CreateFollowUpIssue(context.Background(), tracker.FollowUpIssue{
				Repo:   "acme/widgets",
				Title:  "Follow up",
				Body:   "details",
				Labels: []string{"odin:ready"},
			})
			return err
		},
	} {
		if err := run(); !errors.Is(err, tracker.ErrMutationUnsupported) {
			t.Fatalf("%s error = %v, want ErrMutationUnsupported", name, err)
		}
	}
	if requests != 0 {
		t.Fatalf("mutation requests = %d, want 0", requests)
	}
}

func TestGitHubErrorsRedactTokenLikeStrings(t *testing.T) {
	t.Parallel()

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

func TestLifecycleMarkersReturnMutationUnsupported(t *testing.T) {
	t.Parallel()

	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests++
		t.Fatalf("mutation unexpectedly called GitHub: %s %s", request.Method, request.URL.Path)
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
		if err := run(context.Background(), id); !errors.Is(err, tracker.ErrMutationUnsupported) {
			t.Fatalf("%s marker error = %v, want ErrMutationUnsupported", name, err)
		}
	}
	if requests != 0 {
		t.Fatalf("mutation requests = %d, want 0", requests)
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
