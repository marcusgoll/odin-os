package intake

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"odin-os/internal/core/projects"
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
		ProjectID:  projectID,
		Provider:   provider,
		Repo:       repo,
		Number:     issue.Number,
		Title:      issue.Title,
		BodyHash:   "sha256:" + sha256Hex(issue.Body),
		URL:        issue.URL,
		State:      state,
		LabelsJSON: string(labelsJSON),
		SyncStatus: "eligible",
	}
}

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
