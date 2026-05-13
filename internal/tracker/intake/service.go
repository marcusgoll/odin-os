package intake

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"odin-os/internal/cli/scope"
	"odin-os/internal/core/projects"
	"odin-os/internal/runtime/jobs"
	"odin-os/internal/store/sqlite"
	"odin-os/internal/tracker"
	trackergithub "odin-os/internal/tracker/github"
)

const defaultGitHubTokenEnv = "GITHUB_TOKEN"

type TrackerFactory func(project projects.Manifest, options SyncOptions) (tracker.Tracker, error)

type Service struct {
	Store      *sqlite.Store
	Registry   projects.Registry
	NewTracker TrackerFactory
}

type SyncOptions struct {
	ProjectKey string
	DryRun     bool
}

type SyncSummary struct {
	ProjectKey string
	Repo       string
	Fetched    int
	Persisted  int
	DryRun     bool
}

type ReconcileOptions struct {
	ProjectKey string
}

type ReconcileSummary struct {
	ProjectKey          string
	Repo                string
	Eligible            int
	Created             int
	Existing            int
	Linked              int
	Dispatched          bool
	PullRequestsCreated bool
}

func (service Service) SyncProject(ctx context.Context, options SyncOptions) (SyncSummary, error) {
	if service.Store == nil {
		return SyncSummary{}, fmt.Errorf("intake store is required")
	}
	project, ok := service.Registry.Lookup(strings.TrimSpace(options.ProjectKey))
	if !ok {
		return SyncSummary{}, fmt.Errorf("unknown project %q", options.ProjectKey)
	}
	if project.ProjectClass != projects.ProjectClassGitHubBacked || strings.TrimSpace(project.GitHub.Repo) == "" {
		return SyncSummary{}, fmt.Errorf("project %q is not a GitHub-backed intake source", project.Key)
	}

	factory := service.NewTracker
	if factory == nil {
		factory = NewGitHubTracker
	}
	source, err := factory(project, options)
	if err != nil {
		return SyncSummary{}, err
	}

	issues, err := source.FetchEligibleIssues(ctx)
	if err != nil {
		return SyncSummary{}, err
	}

	summary := SyncSummary{
		ProjectKey: project.Key,
		Repo:       project.GitHub.Repo,
		Fetched:    len(issues),
		DryRun:     options.DryRun,
	}
	if options.DryRun {
		return summary, nil
	}

	runtimeProject, err := service.ensureRuntimeProject(ctx, project)
	if err != nil {
		return SyncSummary{}, err
	}
	for _, issue := range issues {
		if _, err := service.Store.UpsertExternalIssue(ctx, mapIssue(runtimeProject.ID, issue, project.GitHub.Repo)); err != nil {
			return SyncSummary{}, err
		}
		summary.Persisted++
	}
	return summary, nil
}

func (service Service) ReconcileProject(ctx context.Context, options ReconcileOptions) (ReconcileSummary, error) {
	if service.Store == nil {
		return ReconcileSummary{}, fmt.Errorf("intake store is required")
	}
	project, ok := service.Registry.Lookup(strings.TrimSpace(options.ProjectKey))
	if !ok {
		return ReconcileSummary{}, fmt.Errorf("unknown project %q", options.ProjectKey)
	}
	if project.ProjectClass != projects.ProjectClassGitHubBacked || strings.TrimSpace(project.GitHub.Repo) == "" {
		return ReconcileSummary{}, fmt.Errorf("project %q is not a GitHub-backed intake source", project.Key)
	}

	runtimeProject, err := service.ensureRuntimeProject(ctx, project)
	if err != nil {
		return ReconcileSummary{}, err
	}
	issues, err := service.Store.ListExternalIssues(ctx, sqlite.ListExternalIssuesParams{
		ProjectID:  &runtimeProject.ID,
		Repo:       project.GitHub.Repo,
		SyncStatus: "eligible",
	})
	if err != nil {
		return ReconcileSummary{}, err
	}

	summary := ReconcileSummary{
		ProjectKey: project.Key,
		Repo:       project.GitHub.Repo,
		Eligible:   len(issues),
	}
	jobService := jobs.Service{
		Store:    service.Store,
		Registry: service.Registry,
	}
	resolved := scope.Resolution{
		Kind:       scope.ScopeProject,
		ProjectKey: project.Key,
	}
	for _, issue := range issues {
		criteria := sqlite.NormalizeAcceptanceCriteria(issue.AcceptanceCriteria)
		result, err := jobService.CreateTaskOnce(ctx, jobs.CreateTaskParams{
			Resolved:           resolved,
			Key:                externalIssueTaskKey(issue),
			Title:              issue.Title,
			AcceptanceCriteria: criteria,
			RequestedBy:        "github_issue_intake",
		})
		if err != nil {
			return ReconcileSummary{}, err
		}
		if len(criteria) > 0 {
			task, err := service.Store.UpdateTaskAcceptanceCriteria(ctx, result.Task.ID, criteria)
			if err != nil {
				return ReconcileSummary{}, err
			}
			result.Task = task
		}
		if result.Created {
			summary.Created++
		} else {
			summary.Existing++
		}

		linked, err := service.linkExternalIssueToTask(ctx, result.Task, issue)
		if err != nil {
			return ReconcileSummary{}, err
		}
		if linked {
			summary.Linked++
		}
		if _, err := service.Store.UpsertExternalIssue(ctx, sqlite.UpsertExternalIssueParams{
			ProjectID:          issue.ProjectID,
			Provider:           issue.Provider,
			Repo:               issue.Repo,
			Number:             issue.Number,
			Title:              issue.Title,
			BodyHash:           issue.BodyHash,
			URL:                issue.URL,
			State:              issue.State,
			LabelsJSON:         issue.LabelsJSON,
			SyncStatus:         "reconciled",
			SyncCursor:         issue.SyncCursor,
			AcceptanceCriteria: issue.AcceptanceCriteria,
		}); err != nil {
			return ReconcileSummary{}, err
		}
	}

	return summary, nil
}

func NewGitHubTracker(project projects.Manifest, options SyncOptions) (tracker.Tracker, error) {
	owner, repo, ok := strings.Cut(project.GitHub.Repo, "/")
	if !ok || owner == "" || repo == "" {
		return nil, fmt.Errorf("invalid GitHub repo %q", project.GitHub.Repo)
	}
	return trackergithub.NewClientWithConfig(trackergithub.Config{
		BaseURL:  os.Getenv("ODIN_GITHUB_API_BASE_URL"),
		Owner:    owner,
		Repo:     repo,
		TokenEnv: defaultGitHubTokenEnv,
		DryRun:   options.DryRun,
	}), nil
}

func (service Service) ensureRuntimeProject(ctx context.Context, manifest projects.Manifest) (sqlite.Project, error) {
	project, err := service.Store.GetProjectByKey(ctx, manifest.Key)
	if err == nil {
		return project, nil
	}
	if err != sql.ErrNoRows {
		return sqlite.Project{}, err
	}
	scopeValue := "project"
	if manifest.SystemProject {
		scopeValue = "odin-core"
	}
	return service.Store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           manifest.Key,
		Name:          manifest.Name,
		Scope:         scopeValue,
		GitRoot:       manifest.GitRoot,
		DefaultBranch: manifest.DefaultBranch,
		GitHubRepo:    manifest.GitHub.Repo,
		ManifestPath:  manifest.SourcePath,
	})
}

func (service Service) linkExternalIssueToTask(ctx context.Context, task sqlite.Task, issue sqlite.ExternalIssue) (bool, error) {
	payload, err := json.Marshal(externalIssueIntakePayload{
		ExternalIssueID:    issue.ID,
		Provider:           issue.Provider,
		Repo:               issue.Repo,
		Number:             issue.Number,
		Title:              issue.Title,
		BodyHash:           issue.BodyHash,
		URL:                issue.URL,
		State:              issue.State,
		LabelsJSON:         issue.LabelsJSON,
		SyncStatus:         issue.SyncStatus,
		SyncCursor:         externalIssueDedupKey(issue),
		AcceptanceCriteria: issue.AcceptanceCriteria,
	})
	if err != nil {
		return false, err
	}
	if _, err := service.Store.CreateTaskIntake(ctx, sqlite.CreateTaskIntakeParams{
		TaskID:      task.ID,
		Source:      "github_issue",
		IntakeType:  "external_issue",
		DedupKey:    externalIssueDedupKey(issue),
		RequestedBy: "github_issue_intake",
		PayloadJSON: string(payload),
	}); err != nil {
		if errors.Is(err, sqlite.ErrTaskIntakeConflict) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

type externalIssueIntakePayload struct {
	ExternalIssueID    int64    `json:"external_issue_id"`
	Provider           string   `json:"provider"`
	Repo               string   `json:"repo"`
	Number             int      `json:"number"`
	Title              string   `json:"title"`
	BodyHash           string   `json:"body_hash"`
	URL                string   `json:"url"`
	State              string   `json:"state"`
	LabelsJSON         string   `json:"labels_json"`
	SyncStatus         string   `json:"sync_status"`
	SyncCursor         string   `json:"sync_cursor"`
	AcceptanceCriteria []string `json:"acceptance_criteria,omitempty"`
}

func externalIssueTaskKey(issue sqlite.ExternalIssue) string {
	provider := strings.TrimSpace(issue.Provider)
	if provider == "" {
		provider = "external"
	}
	providerKey := slugKeyPart(provider)
	if providerKey == "" {
		providerKey = "external"
	}
	return fmt.Sprintf("%s-issue-%d", providerKey, issue.Number)
}

func externalIssueDedupKey(issue sqlite.ExternalIssue) string {
	if cursor := strings.TrimSpace(issue.SyncCursor); cursor != "" {
		return cursor
	}
	provider := strings.TrimSpace(issue.Provider)
	if provider == "" {
		provider = "external"
	}
	return fmt.Sprintf("%s:issue:%s:%d", provider, issue.Repo, issue.Number)
}

func slugKeyPart(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var builder strings.Builder
	lastDash := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			builder.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			builder.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(builder.String(), "-")
}

func mapIssue(projectID int64, issue tracker.Issue, fallbackRepo string) sqlite.UpsertExternalIssueParams {
	repo := issue.Repo
	if repo == "" {
		repo = fallbackRepo
	}
	provider := issue.Provider
	if provider == "" {
		provider = "github"
	}
	state := issue.State
	if state == "" {
		state = "open"
	}
	labelsJSON, _ := json.Marshal(issue.Labels)
	return sqlite.UpsertExternalIssueParams{
		ProjectID:          projectID,
		Provider:           provider,
		Repo:               repo,
		Number:             issue.Number,
		Title:              issue.Title,
		BodyHash:           "sha256:" + sha256Hex(issue.Body),
		URL:                issue.URL,
		State:              state,
		LabelsJSON:         string(labelsJSON),
		SyncStatus:         "eligible",
		SyncCursor:         fmt.Sprintf("%s:issue:%s:%d", provider, repo, issue.Number),
		AcceptanceCriteria: extractAcceptanceCriteria(issue.Body),
	}
}

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func extractAcceptanceCriteria(body string) []string {
	lines := strings.Split(strings.ReplaceAll(strings.ReplaceAll(body, "\r\n", "\n"), "\r", "\n"), "\n")
	inSection := false
	var criteria []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			heading := strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
			normalizedHeading := strings.ToLower(strings.TrimSuffix(heading, ":"))
			if inSection {
				break
			}
			inSection = normalizedHeading == "acceptance criteria"
			continue
		}
		if !inSection || trimmed == "" {
			continue
		}
		if criterion, ok := acceptanceCriterionLine(trimmed); ok {
			criteria = append(criteria, criterion)
		}
	}
	return sqlite.NormalizeAcceptanceCriteria(criteria)
}

func acceptanceCriterionLine(line string) (string, bool) {
	line = strings.TrimSpace(line)
	for _, prefix := range []string{"- ", "* "} {
		if strings.HasPrefix(line, prefix) {
			return stripGitHubCheckbox(strings.TrimSpace(strings.TrimPrefix(line, prefix))), true
		}
	}
	index := 0
	for index < len(line) && line[index] >= '0' && line[index] <= '9' {
		index++
	}
	if index > 0 && index+1 < len(line) && line[index] == '.' && line[index+1] == ' ' {
		return stripGitHubCheckbox(strings.TrimSpace(line[index+2:])), true
	}
	return "", false
}

func stripGitHubCheckbox(line string) string {
	lower := strings.ToLower(line)
	for _, checkbox := range []string{"[ ] ", "[x] "} {
		if strings.HasPrefix(lower, checkbox) {
			return strings.TrimSpace(line[len(checkbox):])
		}
	}
	return strings.TrimSpace(line)
}
