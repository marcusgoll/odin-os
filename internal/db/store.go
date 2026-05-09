package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"odin-os/internal/runtime/events"
	"odin-os/internal/store/sqlite"
)

var ErrRepositoryNotMigrated = errors.New("repository target not migrated")

// Store is the agency persistence boundary. SQLite remains the backing runtime
// authority while target repositories are introduced incrementally.
type Store interface {
	RuntimeRepository
	Close() error
}

type RuntimeRepository interface {
	Health(ctx context.Context) error
	GetAgentRun(ctx context.Context, id int64) (AgentRun, error)
	ListAgentRuns(ctx context.Context, filter AgentRunFilter) ([]AgentRun, error)
	ListRunEvents(ctx context.Context, filter RunEventFilter) ([]RunEvent, error)
	GetWorkspace(ctx context.Context, id int64) (Workspace, error)
	GetFailure(ctx context.Context, id int64) (Failure, error)
	ListIssues(ctx context.Context, filter IssueFilter) ([]Issue, error)
	ListPullRequests(ctx context.Context, filter PullRequestFilter) ([]PullRequest, error)
	ListLocks(ctx context.Context, filter LockFilter) ([]Lock, error)
}

type Issue struct {
	ID       int64
	Provider string
	Repo     string
	Number   int
	Title    string
	Status   string
	Cursor   string
}

type IssueFilter struct {
	Repo   string
	Status string
}

type PullRequest struct {
	ID     int64
	Repo   string
	Number int
	Status string
	URL    string
}

type PullRequestFilter struct {
	Repo   string
	Status string
}

type Lock struct {
	ID        int64
	Key       string
	Owner     string
	Status    string
	ExpiresAt *time.Time
}

type LockFilter struct {
	Key    string
	Status string
}

type AgentRun struct {
	ID         int64
	WorkItemID int64
	Executor   string
	Status     string
	Attempt    int
	StartedAt  time.Time
	FinishedAt *time.Time
	Summary    string
}

type AgentRunFilter struct {
	Status string
}

type RunEvent struct {
	ID          int64
	Type        string
	StreamType  string
	StreamID    int64
	ProjectID   *int64
	WorkItemID  *int64
	AgentRunID  *int64
	PayloadJSON []byte
	OccurredAt  time.Time
}

type RunEventFilter struct {
	ProjectID  *int64
	WorkItemID *int64
	AgentRunID *int64
}

type Workspace struct {
	ID           int64
	ProjectID    int64
	WorkItemID   int64
	AgentRunID   int64
	Mode         string
	BranchName   string
	WorktreePath string
	RepoRoot     string
	State        string
	HeartbeatAt  time.Time
	ReleasedAt   *time.Time
	CleanedUpAt  *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type Failure struct {
	ID          int64
	AgentRunID  *int64
	Severity    string
	Status      string
	Summary     string
	DetailsJSON string
	OpenedAt    time.Time
	UpdatedAt   time.Time
}

type SQLiteRepository struct {
	store *sqlite.Store
}

func NewSQLiteRepository(store *sqlite.Store) *SQLiteRepository {
	return &SQLiteRepository{store: store}
}

func (repository *SQLiteRepository) Close() error {
	if repository.store == nil {
		return nil
	}
	return repository.store.Close()
}

func (repository *SQLiteRepository) Health(ctx context.Context) error {
	if repository.store == nil {
		return errors.New("sqlite store is nil")
	}
	return repository.store.DB().PingContext(ctx)
}

func (repository *SQLiteRepository) GetAgentRun(ctx context.Context, id int64) (AgentRun, error) {
	run, err := repository.store.GetRun(ctx, id)
	if err != nil {
		return AgentRun{}, err
	}
	return mapAgentRun(run), nil
}

func (repository *SQLiteRepository) ListAgentRuns(ctx context.Context, filter AgentRunFilter) ([]AgentRun, error) {
	if filter.Status == "" {
		return nil, explicitNotMigrated("agent run list without status")
	}
	runs, err := repository.store.ListRunsByStatus(ctx, filter.Status)
	if err != nil {
		return nil, err
	}
	agentRuns := make([]AgentRun, 0, len(runs))
	for _, run := range runs {
		agentRuns = append(agentRuns, mapAgentRun(run))
	}
	return agentRuns, nil
}

func (repository *SQLiteRepository) ListRunEvents(ctx context.Context, filter RunEventFilter) ([]RunEvent, error) {
	records, err := repository.store.ListEvents(ctx, sqlite.ListEventsParams{
		ProjectID: filter.ProjectID,
		TaskID:    filter.WorkItemID,
		RunID:     filter.AgentRunID,
	})
	if err != nil {
		return nil, err
	}
	runEvents := make([]RunEvent, 0, len(records))
	for _, record := range records {
		runEvents = append(runEvents, mapRunEvent(record))
	}
	return runEvents, nil
}

func (repository *SQLiteRepository) GetWorkspace(ctx context.Context, id int64) (Workspace, error) {
	lease, err := repository.store.GetWorktreeLease(ctx, id)
	if err != nil {
		return Workspace{}, err
	}
	return Workspace{
		ID:           lease.ID,
		ProjectID:    lease.ProjectID,
		WorkItemID:   lease.TaskID,
		AgentRunID:   lease.RunID,
		Mode:         lease.Mode,
		BranchName:   lease.BranchName,
		WorktreePath: lease.WorktreePath,
		RepoRoot:     lease.RepoRoot,
		State:        lease.State,
		HeartbeatAt:  lease.HeartbeatAt,
		ReleasedAt:   lease.ReleasedAt,
		CleanedUpAt:  lease.CleanedUpAt,
		CreatedAt:    lease.CreatedAt,
		UpdatedAt:    lease.UpdatedAt,
	}, nil
}

func (repository *SQLiteRepository) GetFailure(ctx context.Context, id int64) (Failure, error) {
	incident, err := repository.store.GetIncident(ctx, id)
	if err != nil {
		return Failure{}, err
	}
	return Failure{
		ID:          incident.ID,
		AgentRunID:  incident.RunID,
		Severity:    incident.Severity,
		Status:      incident.Status,
		Summary:     incident.Summary,
		DetailsJSON: incident.DetailsJSON,
		OpenedAt:    incident.OpenedAt,
		UpdatedAt:   incident.UpdatedAt,
	}, nil
}

func (repository *SQLiteRepository) ListIssues(ctx context.Context, filter IssueFilter) ([]Issue, error) {
	records, err := repository.store.ListExternalIssues(ctx, sqlite.ListExternalIssuesParams{
		Repo:       filter.Repo,
		SyncStatus: filter.Status,
	})
	if err != nil {
		return nil, err
	}
	issues := make([]Issue, 0, len(records))
	for _, record := range records {
		issues = append(issues, Issue{
			ID:       record.ID,
			Provider: record.Provider,
			Repo:     record.Repo,
			Number:   record.Number,
			Title:    record.Title,
			Status:   record.SyncStatus,
			Cursor:   record.SyncCursor,
		})
	}
	return issues, nil
}

func (repository *SQLiteRepository) ListPullRequests(ctx context.Context, filter PullRequestFilter) ([]PullRequest, error) {
	records, err := repository.store.ListPullRequestHandoffs(ctx, sqlite.ListPullRequestHandoffsParams{
		Repo:        filter.Repo,
		ReviewState: filter.Status,
	})
	if err != nil {
		return nil, err
	}
	pullRequests := make([]PullRequest, 0, len(records))
	for _, record := range records {
		pullRequests = append(pullRequests, PullRequest{
			ID:     record.ID,
			Repo:   record.Repo,
			Number: record.Number,
			Status: record.ReviewState,
			URL:    record.URL,
		})
	}
	return pullRequests, nil
}

func (repository *SQLiteRepository) ListLocks(context.Context, LockFilter) ([]Lock, error) {
	return nil, explicitNotMigrated("locks")
}

func mapAgentRun(run sqlite.Run) AgentRun {
	return AgentRun{
		ID:         run.ID,
		WorkItemID: run.TaskID,
		Executor:   run.Executor,
		Status:     run.Status,
		Attempt:    run.Attempt,
		StartedAt:  run.StartedAt,
		FinishedAt: run.FinishedAt,
		Summary:    run.Summary,
	}
}

func mapRunEvent(record events.Record) RunEvent {
	return RunEvent{
		ID:          record.ID,
		Type:        string(record.Type),
		StreamType:  string(record.StreamType),
		StreamID:    record.StreamID,
		ProjectID:   record.ProjectID,
		WorkItemID:  record.TaskID,
		AgentRunID:  record.RunID,
		PayloadJSON: []byte(record.Payload),
		OccurredAt:  record.OccurredAt,
	}
}

func explicitNotMigrated(name string) error {
	return fmt.Errorf("%w: %s repository has no current SQLite table", ErrRepositoryNotMigrated, name)
}
