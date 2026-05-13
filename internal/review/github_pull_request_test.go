package review

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGitHubPullRequestManagerCreatesUpdatesAndCommentsWithFixtures(t *testing.T) {
	t.Parallel()

	var requests []string
	findCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatalf("ReadAll() error = %v", err)
		}
		requests = append(requests, request.Method+" "+request.URL.RequestURI()+" "+string(body))

		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/repos/acme/widgets/pulls":
			findCalls++
			if got := request.URL.Query().Get("head"); got != "acme:feature-branch" {
				t.Fatalf("head query = %q, want acme:feature-branch", got)
			}
			response.Header().Set("Content-Type", "application/json")
			if findCalls == 1 {
				fmt.Fprint(response, `[]`)
				return
			}
			fmt.Fprint(response, `[{"number":42,"html_url":"https://github.example/acme/widgets/pull/42","state":"open"}]`)
		case request.Method == http.MethodPost && request.URL.Path == "/repos/acme/widgets/pulls":
			response.WriteHeader(http.StatusCreated)
			fmt.Fprint(response, `{"number":42,"html_url":"https://github.example/acme/widgets/pull/42","state":"open"}`)
		case request.Method == http.MethodPatch && request.URL.Path == "/repos/acme/widgets/pulls/42":
			fmt.Fprint(response, `{"number":42,"html_url":"https://github.example/acme/widgets/pull/42","state":"open"}`)
		case request.Method == http.MethodPost && request.URL.Path == "/repos/acme/widgets/issues/42/labels":
			fmt.Fprint(response, `{}`)
		case request.Method == http.MethodPost && request.URL.Path == "/repos/acme/widgets/issues/42/comments":
			response.WriteHeader(http.StatusCreated)
			fmt.Fprint(response, `{}`)
		default:
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL.RequestURI())
		}
	}))
	defer server.Close()

	manager := NewGitHubPullRequestManager(GitHubPullRequestConfig{
		BaseURL:    server.URL,
		Owner:      "acme",
		Repo:       "widgets",
		BaseBranch: "main",
	})

	pr, err := manager.Upsert(context.Background(), PullRequestRequest{
		IssueURL: "https://github.example/acme/widgets/issues/7",
		Title:    "Implement widgets",
		Branch:   "feature-branch",
		Body:     "first body",
		Labels:   []string{"odin:human-review"},
	})
	if err != nil {
		t.Fatalf("Upsert(create) error = %v", err)
	}
	if pr.Provider != "github" || pr.Repo != "acme/widgets" || pr.Number != 42 || pr.State != "open" {
		t.Fatalf("created PR = %+v, want github acme/widgets#42 open", pr)
	}

	updated, err := manager.Upsert(context.Background(), PullRequestRequest{
		Title:  "Implement widgets v2",
		Branch: "feature-branch",
		Body:   "updated body",
	})
	if err != nil {
		t.Fatalf("Upsert(update) error = %v", err)
	}
	if updated.Number != 42 || updated.URL != pr.URL {
		t.Fatalf("updated PR = %+v, want same PR URL/number as create", updated)
	}

	if err := manager.AddComment(context.Background(), PullRequestComment{
		PullRequest: pr,
		Body:        "review summary",
	}); err != nil {
		t.Fatalf("AddComment() error = %v", err)
	}

	for _, want := range []string{
		`GET /repos/acme/widgets/pulls?head=acme%3Afeature-branch&state=open `,
		`POST /repos/acme/widgets/pulls {"base":"main","body":"first body","head":"feature-branch","title":"Implement widgets"}`,
		`POST /repos/acme/widgets/issues/42/labels {"labels":["odin:human-review"]}`,
		`PATCH /repos/acme/widgets/pulls/42 {"body":"updated body","title":"Implement widgets v2"}`,
		`POST /repos/acme/widgets/issues/42/comments {"body":"review summary"}`,
	} {
		if !containsRequest(requests, want) {
			t.Fatalf("requests = %#v, want %q", requests, want)
		}
	}
}

func TestGitHubPullRequestManagerDryRunDoesNotWrite(t *testing.T) {
	t.Parallel()

	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests++
		t.Fatalf("dry-run unexpectedly called GitHub: %s %s", request.Method, request.URL.Path)
	}))
	defer server.Close()

	manager := NewGitHubPullRequestManager(GitHubPullRequestConfig{
		BaseURL:    server.URL,
		Owner:      "acme",
		Repo:       "widgets",
		BaseBranch: "main",
		DryRun:     true,
	})

	pr, err := manager.Upsert(context.Background(), PullRequestRequest{
		Title:  "Dry run",
		Branch: "feature-branch",
		Body:   "body",
	})
	if err != nil {
		t.Fatalf("Upsert(dry-run) error = %v", err)
	}
	if pr.Provider != "github" || pr.Repo != "acme/widgets" || pr.State != "dry-run" {
		t.Fatalf("dry-run PR = %+v, want projected dry-run PR", pr)
	}
	if err := manager.AddComment(context.Background(), PullRequestComment{
		PullRequest: pr,
		Body:        "dry-run comment",
	}); err != nil {
		t.Fatalf("AddComment(dry-run) error = %v", err)
	}
	if requests != 0 {
		t.Fatalf("dry-run requests = %d, want 0", requests)
	}
}

func TestGitHubPullRequestManagerErrorsRedactTokens(t *testing.T) {
	t.Parallel()

	const token = "ghp_1234567890abcdefghijklmnopqrstuvwx"
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(response, `{"message":"token %s failed"}`, token)
	}))
	defer server.Close()

	manager := NewGitHubPullRequestManager(GitHubPullRequestConfig{
		BaseURL: server.URL,
		Owner:   "acme",
		Repo:    "widgets",
		Token:   token,
	})

	_, err := manager.Upsert(context.Background(), PullRequestRequest{
		Title:  "Redact",
		Branch: "feature-branch",
		Body:   "body",
	})
	if err == nil {
		t.Fatal("Upsert() error = nil, want redacted GitHub error")
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("error = %q, want token redacted", err.Error())
	}
	if !strings.Contains(err.Error(), "[REDACTED]") {
		t.Fatalf("error = %q, want redaction marker", err.Error())
	}
}

func TestGitHubPullRequestManagerImplementsCanonicalInterface(t *testing.T) {
	t.Parallel()

	var _ PullRequestManager = NewGitHubPullRequestManager(GitHubPullRequestConfig{})
}

func containsRequest(requests []string, want string) bool {
	for _, request := range requests {
		if request == want {
			return true
		}
	}
	return false
}
