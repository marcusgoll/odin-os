package review

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
)

const (
	githubProvider       = "github"
	defaultGitHubBaseURL = "https://api.github.com"
)

var githubTokenLikePattern = regexp.MustCompile(`\b(?:gh[pousr]_[A-Za-z0-9_]{20,}|github_pat_[A-Za-z0-9_]{20,})\b`)

type GitHubPullRequestConfig struct {
	BaseURL    string
	Owner      string
	Repo       string
	BaseBranch string
	Token      string
	TokenEnv   string
	DryRun     bool
}

type GitHubHTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type GitHubPullRequestManager struct {
	config GitHubPullRequestConfig
	doer   GitHubHTTPDoer
}

func NewGitHubPullRequestManager(config GitHubPullRequestConfig) *GitHubPullRequestManager {
	return NewGitHubPullRequestManagerWithDoer(config, &http.Client{Timeout: 30 * time.Second})
}

func NewGitHubPullRequestManagerWithDoer(config GitHubPullRequestConfig, doer GitHubHTTPDoer) *GitHubPullRequestManager {
	if doer == nil {
		doer = &http.Client{Timeout: 30 * time.Second}
	}
	return &GitHubPullRequestManager{
		config: normalizeGitHubPullRequestConfig(config),
		doer:   doer,
	}
}

func (manager *GitHubPullRequestManager) Upsert(ctx context.Context, request PullRequestRequest) (PullRequest, error) {
	if manager.config.DryRun {
		return PullRequest{
			Provider: githubProvider,
			Repo:     manager.repoID(),
			State:    "dry-run",
		}, nil
	}
	if !manager.configured() {
		return PullRequest{}, fmt.Errorf("GitHub pull request manager is not configured")
	}

	existing, found, err := manager.findOpenPullRequest(ctx, request.Branch)
	if err != nil {
		return PullRequest{}, err
	}
	if found {
		pr, err := manager.updatePullRequest(ctx, existing.Number, request)
		if err != nil {
			return PullRequest{}, err
		}
		return pr, manager.applyLabels(ctx, pr.Number, request.Labels)
	}

	pr, err := manager.createPullRequest(ctx, request)
	if err != nil {
		return PullRequest{}, err
	}
	return pr, manager.applyLabels(ctx, pr.Number, request.Labels)
}

func (manager *GitHubPullRequestManager) AddComment(ctx context.Context, request PullRequestComment) error {
	if manager.config.DryRun {
		return nil
	}
	repo := request.PullRequest.Repo
	if repo == "" {
		repo = manager.repoID()
	}
	owner, repoName, err := splitRepo(repo)
	if err != nil {
		return err
	}
	if request.PullRequest.Number <= 0 {
		return fmt.Errorf("pull request number is required")
	}
	path := fmt.Sprintf("/repos/%s/%s/issues/%d/comments", url.PathEscape(owner), url.PathEscape(repoName), request.PullRequest.Number)
	_, err = manager.doJSON(ctx, http.MethodPost, path, nil, commentPayload{Body: request.Body})
	return err
}

func (manager *GitHubPullRequestManager) findOpenPullRequest(ctx context.Context, branch string) (PullRequest, bool, error) {
	query := url.Values{}
	query.Set("state", "open")
	query.Set("head", manager.headQuery(branch))
	path := fmt.Sprintf("/repos/%s/%s/pulls", url.PathEscape(manager.config.Owner), url.PathEscape(manager.config.Repo))
	rawPulls, err := manager.doJSON(ctx, http.MethodGet, path, query, nil)
	if err != nil {
		return PullRequest{}, false, err
	}

	var decoded []githubPullRequest
	if err := json.Unmarshal(rawPulls, &decoded); err != nil {
		return PullRequest{}, false, fmt.Errorf("decode GitHub pull requests: %w", err)
	}
	if len(decoded) == 0 {
		return PullRequest{}, false, nil
	}
	return manager.toPullRequest(decoded[0]), true, nil
}

func (manager *GitHubPullRequestManager) createPullRequest(ctx context.Context, request PullRequestRequest) (PullRequest, error) {
	path := fmt.Sprintf("/repos/%s/%s/pulls", url.PathEscape(manager.config.Owner), url.PathEscape(manager.config.Repo))
	rawPull, err := manager.doJSON(ctx, http.MethodPost, path, nil, pullRequestPayload{
		Base:  manager.config.BaseBranch,
		Body:  request.Body,
		Head:  request.Branch,
		Title: request.Title,
	})
	if err != nil {
		return PullRequest{}, err
	}
	return manager.decodePullRequest(rawPull)
}

func (manager *GitHubPullRequestManager) updatePullRequest(ctx context.Context, number int, request PullRequestRequest) (PullRequest, error) {
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d", url.PathEscape(manager.config.Owner), url.PathEscape(manager.config.Repo), number)
	rawPull, err := manager.doJSON(ctx, http.MethodPatch, path, nil, pullRequestUpdatePayload{
		Body:  request.Body,
		Title: request.Title,
	})
	if err != nil {
		return PullRequest{}, err
	}
	return manager.decodePullRequest(rawPull)
}

func (manager *GitHubPullRequestManager) applyLabels(ctx context.Context, number int, labels []string) error {
	clean := nonEmpty(labels)
	if len(clean) == 0 {
		return nil
	}
	path := fmt.Sprintf("/repos/%s/%s/issues/%d/labels", url.PathEscape(manager.config.Owner), url.PathEscape(manager.config.Repo), number)
	_, err := manager.doJSON(ctx, http.MethodPost, path, nil, labelsPayload{Labels: clean})
	return err
}

func (manager *GitHubPullRequestManager) decodePullRequest(rawPull []byte) (PullRequest, error) {
	var decoded githubPullRequest
	if err := json.Unmarshal(rawPull, &decoded); err != nil {
		return PullRequest{}, fmt.Errorf("decode GitHub pull request: %w", err)
	}
	return manager.toPullRequest(decoded), nil
}

func (manager *GitHubPullRequestManager) toPullRequest(pr githubPullRequest) PullRequest {
	return PullRequest{
		Provider: githubProvider,
		Repo:     manager.repoID(),
		Number:   pr.Number,
		URL:      pr.HTMLURL,
		State:    pr.State,
	}
}

func (manager *GitHubPullRequestManager) doJSON(ctx context.Context, method string, path string, query url.Values, body any) ([]byte, error) {
	endpoint, err := url.Parse(strings.TrimRight(manager.config.BaseURL, "/") + path)
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
	if token := manager.token(); token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}

	response, err := manager.doer.Do(request)
	if err != nil {
		return nil, fmt.Errorf("call GitHub: %s", manager.redact(err.Error()))
	}
	defer response.Body.Close()

	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read GitHub response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return nil, fmt.Errorf("GitHub %s %s returned %d: %s", method, endpoint.Path, response.StatusCode, manager.redact(string(responseBody)))
	}
	return responseBody, nil
}

func (manager *GitHubPullRequestManager) configured() bool {
	return manager.config.Owner != "" && manager.config.Repo != ""
}

func (manager *GitHubPullRequestManager) repoID() string {
	if !manager.configured() {
		return ""
	}
	return manager.config.Owner + "/" + manager.config.Repo
}

func (manager *GitHubPullRequestManager) headQuery(branch string) string {
	if strings.Contains(branch, ":") {
		return branch
	}
	return manager.config.Owner + ":" + branch
}

func (manager *GitHubPullRequestManager) token() string {
	if manager.config.Token != "" {
		return manager.config.Token
	}
	if manager.config.TokenEnv != "" {
		return os.Getenv(manager.config.TokenEnv)
	}
	return ""
}

func (manager *GitHubPullRequestManager) redact(value string) string {
	redacted := value
	if token := manager.token(); token != "" {
		redacted = strings.ReplaceAll(redacted, token, "[REDACTED]")
	}
	return githubTokenLikePattern.ReplaceAllString(redacted, "[REDACTED]")
}

func normalizeGitHubPullRequestConfig(config GitHubPullRequestConfig) GitHubPullRequestConfig {
	if config.BaseURL == "" {
		config.BaseURL = defaultGitHubBaseURL
	}
	if config.BaseBranch == "" {
		config.BaseBranch = "main"
	}
	return config
}

func splitRepo(repoID string) (string, string, error) {
	owner, repo, ok := strings.Cut(repoID, "/")
	if !ok || owner == "" || repo == "" {
		return "", "", fmt.Errorf("invalid GitHub repo %q", repoID)
	}
	return owner, repo, nil
}

type githubPullRequest struct {
	Number  int    `json:"number"`
	HTMLURL string `json:"html_url"`
	State   string `json:"state"`
}

type pullRequestPayload struct {
	Base  string `json:"base"`
	Body  string `json:"body"`
	Head  string `json:"head"`
	Title string `json:"title"`
}

type pullRequestUpdatePayload struct {
	Body  string `json:"body"`
	Title string `json:"title"`
}

type labelsPayload struct {
	Labels []string `json:"labels"`
}

type commentPayload struct {
	Body string `json:"body"`
}
