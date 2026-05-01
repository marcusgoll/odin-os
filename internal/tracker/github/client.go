package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"odin-os/internal/tracker"
)

const (
	defaultBaseURL = "https://api.github.com"
	provider       = "github"
)

var tokenLikePattern = regexp.MustCompile(`\b(?:gh[pousr]_[A-Za-z0-9_]{20,}|github_pat_[A-Za-z0-9_]{20,})\b`)

// Config controls the GitHub Issues adapter. Tokens come from Token or
// TokenEnv and are used only for GitHub API calls, never worker prompts.
type Config struct {
	BaseURL        string
	Owner          string
	Repo           string
	Token          string
	TokenEnv       string
	EligibleLabels []string
	BlockedLabels  []string
	DryRun         bool
}

// HTTPDoer is the small net/http boundary used by tests and the adapter.
type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

// Client wraps GitHub Issues/PR intake and mutation behavior behind the tracker
// interface. A zero-value client preserves the previous placeholder behavior.
type Client struct {
	config Config
	doer   HTTPDoer
	audit  tracker.RequestAudit
}

func NewClient() *Client {
	return &Client{doer: http.DefaultClient}
}

func NewClientWithConfig(config Config) *Client {
	return NewClientWithConfigAndDoer(config, &http.Client{Timeout: 30 * time.Second})
}

func NewClientWithConfigAndDoer(config Config, doer HTTPDoer) *Client {
	if doer == nil {
		doer = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{
		config: normalizeConfig(config),
		doer:   doer,
	}
}

// ListEligibleIssues preserves the previous read-only method name while callers
// migrate to FetchEligibleIssues.
func (client *Client) ListEligibleIssues(ctx context.Context) ([]tracker.Issue, error) {
	return client.FetchEligibleIssues(ctx)
}

func (client *Client) FetchEligibleIssues(ctx context.Context) ([]tracker.Issue, error) {
	if !client.configured() {
		return nil, tracker.ErrNotImplemented
	}

	path := fmt.Sprintf("/repos/%s/%s/issues", url.PathEscape(client.config.Owner), url.PathEscape(client.config.Repo))
	query := url.Values{}
	query.Set("state", "open")
	rawIssues, err := client.doJSON(ctx, http.MethodGet, path, query, nil)
	if err != nil {
		return nil, err
	}

	var decoded []githubIssue
	if err := json.Unmarshal(rawIssues, &decoded); err != nil {
		return nil, fmt.Errorf("decode GitHub issues: %w", err)
	}

	issues := make([]tracker.Issue, 0, len(decoded))
	for _, issue := range decoded {
		if issue.PullRequest != nil {
			continue
		}
		if issue.State != "open" {
			continue
		}
		labels := labelNames(issue.Labels)
		if !hasAllLabels(labels, client.config.EligibleLabels) || hasAnyLabel(labels, client.config.BlockedLabels) {
			continue
		}
		issues = append(issues, client.toTrackerIssue(issue, labels))
	}
	return issues, nil
}

func (client *Client) FetchIssueByID(ctx context.Context, id tracker.IssueID) (tracker.Issue, error) {
	if !client.configured() {
		return tracker.Issue{}, tracker.ErrNotImplemented
	}
	owner, repo, err := client.resolveRepo(id.Repo)
	if err != nil {
		return tracker.Issue{}, err
	}
	path := fmt.Sprintf("/repos/%s/%s/issues/%d", url.PathEscape(owner), url.PathEscape(repo), id.Number)
	rawIssue, err := client.doJSON(ctx, http.MethodGet, path, nil, nil)
	if err != nil {
		return tracker.Issue{}, err
	}
	var decoded githubIssue
	if err := json.Unmarshal(rawIssue, &decoded); err != nil {
		return tracker.Issue{}, fmt.Errorf("decode GitHub issue: %w", err)
	}
	return client.toTrackerIssue(decoded, labelNames(decoded.Labels)), nil
}

func (client *Client) FetchIssueComments(ctx context.Context, id tracker.IssueID) ([]tracker.IssueComment, error) {
	if !client.configured() {
		return nil, tracker.ErrNotImplemented
	}
	owner, repo, err := client.resolveRepo(id.Repo)
	if err != nil {
		return nil, err
	}
	path := fmt.Sprintf("/repos/%s/%s/issues/%d/comments", url.PathEscape(owner), url.PathEscape(repo), id.Number)
	rawComments, err := client.doJSON(ctx, http.MethodGet, path, nil, nil)
	if err != nil {
		return nil, err
	}
	var decoded []githubComment
	if err := json.Unmarshal(rawComments, &decoded); err != nil {
		return nil, fmt.Errorf("decode GitHub issue comments: %w", err)
	}
	comments := make([]tracker.IssueComment, 0, len(decoded))
	for _, comment := range decoded {
		comments = append(comments, tracker.IssueComment{
			Body: comment.Body,
			URL:  comment.HTMLURL,
		})
	}
	return comments, nil
}

func (client *Client) MarkInProgress(ctx context.Context, id tracker.IssueID) error {
	return client.addLabels(ctx, id, []string{tracker.LabelRunning})
}

func (client *Client) MarkBlocked(ctx context.Context, id tracker.IssueID, reason string) error {
	if err := client.addLabels(ctx, id, []string{tracker.LabelBlocked}); err != nil {
		return err
	}
	if strings.TrimSpace(reason) == "" {
		return nil
	}
	return client.AddComment(ctx, id, reason)
}

func (client *Client) MarkFailed(ctx context.Context, id tracker.IssueID, reason string) error {
	if err := client.addLabels(ctx, id, []string{tracker.LabelFailed}); err != nil {
		return err
	}
	if strings.TrimSpace(reason) == "" {
		return nil
	}
	return client.AddComment(ctx, id, reason)
}

func (client *Client) MarkReadyForReview(ctx context.Context, id tracker.IssueID) error {
	return client.addLabels(ctx, id, []string{tracker.LabelHumanReview})
}

func (client *Client) RemoveLabel(ctx context.Context, id tracker.IssueID, label string) error {
	if client.config.DryRun {
		return nil
	}
	if !client.configured() {
		return tracker.ErrNotImplemented
	}
	owner, repo, err := client.resolveRepo(id.Repo)
	if err != nil {
		return err
	}
	path := fmt.Sprintf("/repos/%s/%s/issues/%d/labels/%s", url.PathEscape(owner), url.PathEscape(repo), id.Number, url.PathEscape(label))
	_, err = client.doJSON(ctx, http.MethodDelete, path, nil, nil)
	return err
}

func (client *Client) MarkDone(ctx context.Context, id tracker.IssueID) error {
	if client.config.DryRun {
		return nil
	}
	if err := client.addLabels(ctx, id, []string{tracker.LabelDone}); err != nil {
		return err
	}
	owner, repo, err := client.resolveRepo(id.Repo)
	if err != nil {
		return err
	}
	path := fmt.Sprintf("/repos/%s/%s/issues/%d", url.PathEscape(owner), url.PathEscape(repo), id.Number)
	_, err = client.doJSON(ctx, http.MethodPatch, path, nil, map[string]string{"state": "closed"})
	return err
}

func (client *Client) AddComment(ctx context.Context, id tracker.IssueID, body string) error {
	_, err := client.AddCommentWithResult(ctx, id, body)
	return err
}

func (client *Client) AddCommentWithResult(ctx context.Context, id tracker.IssueID, body string) (tracker.IssueComment, error) {
	if client.config.DryRun {
		return tracker.IssueComment{Body: body}, nil
	}
	owner, repo, err := client.resolveRepo(id.Repo)
	if err != nil {
		return tracker.IssueComment{}, err
	}
	path := fmt.Sprintf("/repos/%s/%s/issues/%d/comments", url.PathEscape(owner), url.PathEscape(repo), id.Number)
	rawComment, err := client.doJSON(ctx, http.MethodPost, path, nil, map[string]string{"body": body})
	if err != nil {
		return tracker.IssueComment{}, err
	}
	var decoded githubComment
	if err := json.Unmarshal(rawComment, &decoded); err != nil {
		return tracker.IssueComment{}, fmt.Errorf("decode GitHub issue comment: %w", err)
	}
	return tracker.IssueComment{Body: decoded.Body, URL: decoded.HTMLURL}, nil
}

func (client *Client) ListPullRequests(ctx context.Context, head string, base string) ([]PullRequest, error) {
	if !client.configured() {
		return nil, tracker.ErrNotImplemented
	}
	path := fmt.Sprintf("/repos/%s/%s/pulls", url.PathEscape(client.config.Owner), url.PathEscape(client.config.Repo))
	query := url.Values{}
	query.Set("state", "open")
	if strings.TrimSpace(head) != "" {
		query.Set("head", client.config.Owner+":"+strings.TrimSpace(head))
	}
	if strings.TrimSpace(base) != "" {
		query.Set("base", strings.TrimSpace(base))
	}
	rawPRs, err := client.doJSON(ctx, http.MethodGet, path, query, nil)
	if err != nil {
		return nil, err
	}
	var decoded []githubPullRequest
	if err := json.Unmarshal(rawPRs, &decoded); err != nil {
		return nil, fmt.Errorf("decode GitHub pull requests: %w", err)
	}
	prs := make([]PullRequest, 0, len(decoded))
	for _, pr := range decoded {
		prs = append(prs, pullRequestFromGitHub(pr))
	}
	return prs, nil
}

func (client *Client) CreatePullRequest(ctx context.Context, request PullRequestRequest) (PullRequest, error) {
	if client.config.DryRun {
		return PullRequest{
			Title:   request.Title,
			Body:    request.Body,
			HeadRef: request.Head,
			BaseRef: request.Base,
			Draft:   request.Draft,
		}, nil
	}
	if !client.configured() {
		return PullRequest{}, tracker.ErrNotImplemented
	}
	path := fmt.Sprintf("/repos/%s/%s/pulls", url.PathEscape(client.config.Owner), url.PathEscape(client.config.Repo))
	rawPR, err := client.doJSON(ctx, http.MethodPost, path, nil, map[string]any{
		"title": request.Title,
		"head":  request.Head,
		"base":  request.Base,
		"body":  request.Body,
		"draft": request.Draft,
	})
	if err != nil {
		return PullRequest{}, err
	}
	var decoded githubPullRequest
	if err := json.Unmarshal(rawPR, &decoded); err != nil {
		return PullRequest{}, fmt.Errorf("decode GitHub pull request: %w", err)
	}
	return pullRequestFromGitHub(decoded), nil
}

func (client *Client) ListWorkflowRuns(ctx context.Context, branch string) ([]WorkflowRun, error) {
	if !client.configured() {
		return nil, tracker.ErrNotImplemented
	}
	path := fmt.Sprintf("/repos/%s/%s/actions/runs", url.PathEscape(client.config.Owner), url.PathEscape(client.config.Repo))
	query := url.Values{}
	if strings.TrimSpace(branch) != "" {
		query.Set("branch", strings.TrimSpace(branch))
	}
	rawRuns, err := client.doJSON(ctx, http.MethodGet, path, query, nil)
	if err != nil {
		return nil, err
	}
	var decoded githubWorkflowRunsResponse
	if err := json.Unmarshal(rawRuns, &decoded); err != nil {
		return nil, fmt.Errorf("decode GitHub workflow runs: %w", err)
	}
	runs := make([]WorkflowRun, 0, len(decoded.WorkflowRuns))
	for _, run := range decoded.WorkflowRuns {
		runs = append(runs, WorkflowRun{
			ID:         run.ID,
			Name:       run.Name,
			Path:       run.Path,
			URL:        run.HTMLURL,
			Status:     run.Status,
			Conclusion: run.Conclusion,
			HeadBranch: run.HeadBranch,
			Event:      run.Event,
		})
	}
	return runs, nil
}

func (client *Client) CreateFollowUpIssue(ctx context.Context, issue tracker.FollowUpIssue) (tracker.Issue, error) {
	repoID := issue.Repo
	if repoID == "" {
		repoID = client.repoID()
	}
	if client.config.DryRun {
		return tracker.Issue{
			Provider: provider,
			Repo:     repoID,
			Title:    issue.Title,
			Body:     issue.Body,
			State:    "dry-run",
			Labels:   append([]string(nil), issue.Labels...),
		}, nil
	}
	if !client.configured() {
		return tracker.Issue{}, tracker.ErrNotImplemented
	}
	owner, repo, err := client.resolveRepo(repoID)
	if err != nil {
		return tracker.Issue{}, err
	}
	path := fmt.Sprintf("/repos/%s/%s/issues", url.PathEscape(owner), url.PathEscape(repo))
	rawIssue, err := client.doJSON(ctx, http.MethodPost, path, nil, map[string]any{
		"title":  issue.Title,
		"body":   issue.Body,
		"labels": issue.Labels,
	})
	if err != nil {
		return tracker.Issue{}, err
	}
	var decoded githubIssue
	if err := json.Unmarshal(rawIssue, &decoded); err != nil {
		return tracker.Issue{}, fmt.Errorf("decode GitHub follow-up issue: %w", err)
	}
	return client.toTrackerIssue(decoded, labelNames(decoded.Labels)), nil
}

func (client *Client) RequestAudit() tracker.RequestAudit {
	audit := client.audit
	audit.Forbidden = append([]tracker.ForbiddenRequest(nil), client.audit.Forbidden...)
	return audit
}

func (client *Client) addLabels(ctx context.Context, id tracker.IssueID, labels []string) error {
	if client.config.DryRun {
		return nil
	}
	if !client.configured() {
		return tracker.ErrNotImplemented
	}
	owner, repo, err := client.resolveRepo(id.Repo)
	if err != nil {
		return err
	}
	path := fmt.Sprintf("/repos/%s/%s/issues/%d/labels", url.PathEscape(owner), url.PathEscape(repo), id.Number)
	_, err = client.doJSON(ctx, http.MethodPost, path, nil, map[string][]string{"labels": labels})
	return err
}

func (client *Client) doJSON(ctx context.Context, method string, path string, query url.Values, body any) ([]byte, error) {
	endpoint, err := url.Parse(strings.TrimRight(client.config.BaseURL, "/") + path)
	if err != nil {
		return nil, fmt.Errorf("build GitHub request URL: %w", err)
	}
	if query != nil {
		endpoint.RawQuery = query.Encode()
	}

	var requestBody io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("encode GitHub request body: %w", err)
		}
		requestBody = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint.String(), requestBody)
	if err != nil {
		return nil, fmt.Errorf("build GitHub request: %w", err)
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if token := client.token(); token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}

	client.recordRequest(method, endpoint.Path)
	response, err := client.doer.Do(request)
	if err != nil {
		return nil, fmt.Errorf("call GitHub: %s", client.redact(err.Error()))
	}
	defer response.Body.Close()

	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read GitHub response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return nil, fmt.Errorf("GitHub %s %s returned %d: %s", method, endpoint.Path, response.StatusCode, client.redact(string(responseBody)))
	}
	return responseBody, nil
}

func (client *Client) recordRequest(method string, path string) {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		client.audit.Reads++
	case http.MethodPost, http.MethodPatch, http.MethodPut, http.MethodDelete:
		client.audit.Writes++
		client.audit.Forbidden = append(client.audit.Forbidden, tracker.ForbiddenRequest{
			Method: method,
			Path:   path,
		})
	default:
		client.audit.Reads++
	}
}

func (client *Client) configured() bool {
	return client.config.Owner != "" && client.config.Repo != ""
}

func (client *Client) resolveRepo(repoID string) (string, string, error) {
	if repoID == "" {
		if client.configured() {
			return client.config.Owner, client.config.Repo, nil
		}
		return "", "", tracker.ErrNotImplemented
	}
	owner, repo, ok := strings.Cut(repoID, "/")
	if !ok || owner == "" || repo == "" {
		return "", "", fmt.Errorf("invalid GitHub repo %q", repoID)
	}
	return owner, repo, nil
}

func (client *Client) repoID() string {
	if !client.configured() {
		return ""
	}
	return client.config.Owner + "/" + client.config.Repo
}

func (client *Client) token() string {
	if client.config.Token != "" {
		return client.config.Token
	}
	if client.config.TokenEnv != "" {
		return os.Getenv(client.config.TokenEnv)
	}
	return ""
}

func (client *Client) redact(value string) string {
	redacted := value
	if token := client.token(); token != "" {
		redacted = strings.ReplaceAll(redacted, token, "[REDACTED]")
	}
	return tokenLikePattern.ReplaceAllString(redacted, "[REDACTED]")
}

func (client *Client) toTrackerIssue(issue githubIssue, labels []string) tracker.Issue {
	return tracker.Issue{
		Provider:    provider,
		Repo:        client.repoID(),
		Number:      issue.Number,
		Title:       issue.Title,
		Body:        issue.Body,
		URL:         issue.HTMLURL,
		State:       issue.State,
		Labels:      labels,
		PullRequest: issue.PullRequest != nil,
	}
}

func normalizeConfig(config Config) Config {
	if config.BaseURL == "" {
		config.BaseURL = defaultBaseURL
	}
	if len(config.EligibleLabels) == 0 {
		config.EligibleLabels = []string{tracker.LabelReady}
	}
	if len(config.BlockedLabels) == 0 {
		config.BlockedLabels = []string{tracker.LabelBlocked}
	}
	return config
}

func labelNames(labels []githubLabel) []string {
	names := make([]string, 0, len(labels))
	for _, label := range labels {
		if label.Name != "" {
			names = append(names, label.Name)
		}
	}
	return names
}

func hasAllLabels(labels []string, required []string) bool {
	for _, requiredLabel := range required {
		if !hasLabel(labels, requiredLabel) {
			return false
		}
	}
	return true
}

func hasAnyLabel(labels []string, blocked []string) bool {
	for _, blockedLabel := range blocked {
		if hasLabel(labels, blockedLabel) {
			return true
		}
	}
	return false
}

func hasLabel(labels []string, want string) bool {
	for _, label := range labels {
		if label == want {
			return true
		}
	}
	return false
}

type githubIssue struct {
	Number      int           `json:"number"`
	Title       string        `json:"title"`
	Body        string        `json:"body"`
	HTMLURL     string        `json:"html_url"`
	State       string        `json:"state"`
	Labels      []githubLabel `json:"labels"`
	PullRequest *struct{}     `json:"pull_request"`
}

type githubLabel struct {
	Name string `json:"name"`
}

type githubComment struct {
	Body    string `json:"body"`
	HTMLURL string `json:"html_url"`
}

type PullRequestRequest struct {
	Title string
	Head  string
	Base  string
	Body  string
	Draft bool
}

type PullRequest struct {
	Number  int
	URL     string
	State   string
	Draft   bool
	Title   string
	Body    string
	HeadRef string
	BaseRef string
}

type WorkflowRun struct {
	ID         int64
	Name       string
	Path       string
	URL        string
	Status     string
	Conclusion string
	HeadBranch string
	Event      string
}

func pullRequestFromGitHub(pr githubPullRequest) PullRequest {
	return PullRequest{
		Number:  pr.Number,
		URL:     pr.HTMLURL,
		State:   pr.State,
		Draft:   pr.Draft,
		Title:   pr.Title,
		Body:    pr.Body,
		HeadRef: pr.Head.Ref,
		BaseRef: pr.Base.Ref,
	}
}

type githubPullRequest struct {
	Number  int    `json:"number"`
	HTMLURL string `json:"html_url"`
	State   string `json:"state"`
	Draft   bool   `json:"draft"`
	Title   string `json:"title"`
	Body    string `json:"body"`
	Head    struct {
		Ref string `json:"ref"`
	} `json:"head"`
	Base struct {
		Ref string `json:"ref"`
	} `json:"base"`
}

type githubWorkflowRunsResponse struct {
	WorkflowRuns []githubWorkflowRun `json:"workflow_runs"`
}

type githubWorkflowRun struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Path       string `json:"path"`
	HTMLURL    string `json:"html_url"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	HeadBranch string `json:"head_branch"`
	Event      string `json:"event"`
}
