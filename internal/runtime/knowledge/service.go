package knowledge

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	runtimeevents "odin-os/internal/runtime/events"
	"odin-os/internal/store/sqlite"
)

const PersistenceNone = "none"

type Service struct {
	Store *sqlite.Store
}

type SearchParams struct {
	Query      string
	ProjectKey string
	Limit      int
}

type SearchResult struct {
	Kind       string
	ID         int64
	Key        string
	ProjectKey string
	Title      string
	Status     string
	Summary    string
	OccurredAt time.Time
	Source     string
}

type SearchResponse struct {
	Query       string
	ProjectKey  string
	ReadOnly    bool
	Persistence string
	Results     []SearchResult
}

type ContextPackParams struct {
	TaskRef    string
	ProjectKey string
	Limit      int
}

type ContextPack struct {
	ObjectType   string
	ObjectID     int64
	ObjectKey    string
	ProjectKey   string
	ReadOnly     bool
	Persistence  string
	Task         TaskContext
	Runs         []RunContext
	Events       []EventContext
	ContextItems []ContextItem
}

type TaskContext struct {
	ID            int64  `json:"id"`
	Key           string `json:"key"`
	Title         string `json:"title"`
	Status        string `json:"status"`
	Scope         string `json:"scope"`
	WorkKind      string `json:"work_kind"`
	RequestedBy   string `json:"requested_by"`
	BlockedReason string `json:"blocked_reason,omitempty"`
}

type RunContext struct {
	ID             int64  `json:"id"`
	Status         string `json:"status"`
	Executor       string `json:"executor"`
	Attempt        int    `json:"attempt"`
	Summary        string `json:"summary,omitempty"`
	TerminalReason string `json:"terminal_reason,omitempty"`
}

type EventContext struct {
	ID         int64
	Type       string
	Scope      string
	Payload    json.RawMessage
	OccurredAt time.Time
}

type ContextItem struct {
	Kind    string `json:"kind"`
	ID      int64  `json:"id"`
	Summary string `json:"summary"`
	Status  string `json:"status"`
}

func (service Service) Search(ctx context.Context, params SearchParams) (SearchResponse, error) {
	if service.Store == nil {
		return SearchResponse{}, fmt.Errorf("knowledge store is required")
	}
	query := strings.TrimSpace(params.Query)
	if query == "" {
		return SearchResponse{}, fmt.Errorf("knowledge search query is required")
	}
	limit := params.Limit
	if limit <= 0 {
		limit = 20
	}
	projectID, projectKey, err := service.resolveProject(ctx, params.ProjectKey)
	if err != nil {
		return SearchResponse{}, err
	}
	matches := make([]SearchResult, 0, limit)
	taskMatches, err := service.searchTasks(ctx, query, projectID, projectKey, limit)
	if err != nil {
		return SearchResponse{}, err
	}
	matches = append(matches, taskMatches...)
	if len(matches) < limit {
		eventMatches, err := service.searchEvents(ctx, query, projectID, projectKey, limit-len(matches))
		if err != nil {
			return SearchResponse{}, err
		}
		matches = append(matches, eventMatches...)
	}
	return SearchResponse{
		Query:       query,
		ProjectKey:  projectKey,
		ReadOnly:    true,
		Persistence: PersistenceNone,
		Results:     matches,
	}, nil
}

func (service Service) BuildContextPack(ctx context.Context, params ContextPackParams) (ContextPack, error) {
	if service.Store == nil {
		return ContextPack{}, fmt.Errorf("knowledge store is required")
	}
	if strings.TrimSpace(params.TaskRef) == "" {
		return ContextPack{}, fmt.Errorf("knowledge context-pack task is required")
	}
	projectID, projectKey, err := service.resolveProject(ctx, params.ProjectKey)
	if err != nil {
		return ContextPack{}, err
	}
	task, err := service.resolveTask(ctx, params.TaskRef, projectID)
	if err != nil {
		return ContextPack{}, err
	}
	if projectKey == "" {
		project, err := service.projectForID(ctx, task.ProjectID)
		if err != nil {
			return ContextPack{}, err
		}
		projectKey = project.Key
	}
	limit := params.Limit
	if limit <= 0 {
		limit = 20
	}
	runs, err := service.runsForTask(ctx, task.ID, limit)
	if err != nil {
		return ContextPack{}, err
	}
	events, err := service.Store.ListEvents(ctx, sqlite.ListEventsParams{TaskID: &task.ID})
	if err != nil {
		return ContextPack{}, err
	}
	if len(events) > limit {
		events = events[len(events)-limit:]
	}
	packets, err := service.Store.ListContextPackets(ctx, sqlite.ListContextPacketsParams{TaskID: &task.ID})
	if err != nil {
		return ContextPack{}, err
	}
	contextItems := make([]ContextItem, 0, len(packets))
	for _, packet := range packets {
		contextItems = append(contextItems, ContextItem{
			Kind:    packet.PacketKind,
			ID:      packet.ID,
			Summary: packet.Summary,
			Status:  packet.Status,
		})
	}
	return ContextPack{
		ObjectType:  "task",
		ObjectID:    task.ID,
		ObjectKey:   task.Key,
		ProjectKey:  projectKey,
		ReadOnly:    true,
		Persistence: PersistenceNone,
		Task: TaskContext{
			ID:            task.ID,
			Key:           task.Key,
			Title:         task.Title,
			Status:        task.Status,
			Scope:         task.Scope,
			WorkKind:      task.WorkKind,
			RequestedBy:   task.RequestedBy,
			BlockedReason: task.BlockedReason,
		},
		Runs:         newRunContexts(runs),
		Events:       newEventContexts(events),
		ContextItems: contextItems,
	}, nil
}

func (service Service) resolveProject(ctx context.Context, projectKey string) (*int64, string, error) {
	projectKey = strings.TrimSpace(projectKey)
	if projectKey == "" {
		return nil, "", nil
	}
	project, err := service.Store.GetProjectByKey(ctx, projectKey)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, "", fmt.Errorf("unknown project %q", projectKey)
		}
		return nil, "", err
	}
	return &project.ID, project.Key, nil
}

func (service Service) projectForID(ctx context.Context, projectID int64) (sqlite.Project, error) {
	row := service.Store.DB().QueryRowContext(ctx, `
		SELECT id, key, name, scope, git_root, default_branch, github_repo, manifest_path, created_at, updated_at
		FROM projects
		WHERE id = ?
	`, projectID)
	var project sqlite.Project
	var createdAt string
	var updatedAt string
	if err := row.Scan(&project.ID, &project.Key, &project.Name, &project.Scope, &project.GitRoot, &project.DefaultBranch, &project.GitHubRepo, &project.ManifestPath, &createdAt, &updatedAt); err != nil {
		return sqlite.Project{}, err
	}
	return project, nil
}

func (service Service) resolveTask(ctx context.Context, taskRef string, projectID *int64) (sqlite.Task, error) {
	taskRef = strings.TrimSpace(taskRef)
	if id, err := strconv.ParseInt(taskRef, 10, 64); err == nil && id > 0 {
		return service.Store.GetTask(ctx, id)
	}
	if projectID == nil {
		return sqlite.Task{}, fmt.Errorf("project is required when task is a key")
	}
	return service.Store.GetTaskByProjectAndKey(ctx, *projectID, taskRef)
}

func (service Service) searchTasks(ctx context.Context, query string, projectID *int64, projectKey string, limit int) ([]SearchResult, error) {
	sqlQuery := `
		SELECT t.id, t.key, t.title, t.status, t.scope, COALESCE(t.summary, ''), t.created_at, p.key
		FROM tasks t
		JOIN projects p ON p.id = t.project_id
		WHERE (LOWER(t.key) LIKE ? OR LOWER(t.title) LIKE ? OR LOWER(COALESCE(t.summary, '')) LIKE ? OR LOWER(COALESCE(t.work_kind, '')) LIKE ?)
	`
	args := []any{like(query), like(query), like(query), like(query)}
	if projectID != nil {
		sqlQuery += ` AND t.project_id = ?`
		args = append(args, *projectID)
	}
	sqlQuery += ` ORDER BY t.id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := service.Store.DB().QueryContext(ctx, sqlQuery, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	results := make([]SearchResult, 0)
	for rows.Next() {
		var result SearchResult
		var scope string
		var createdAt string
		result.Kind = "task"
		result.Source = "runtime.tasks"
		if err := rows.Scan(&result.ID, &result.Key, &result.Title, &result.Status, &scope, &result.Summary, &createdAt, &result.ProjectKey); err != nil {
			return nil, err
		}
		if result.ProjectKey == "" {
			result.ProjectKey = projectKey
		}
		results = append(results, result)
	}
	return results, rows.Err()
}

func (service Service) searchEvents(ctx context.Context, query string, projectID *int64, projectKey string, limit int) ([]SearchResult, error) {
	records, err := service.Store.ListEvents(ctx, sqlite.ListEventsParams{ProjectID: projectID})
	if err != nil {
		return nil, err
	}
	query = strings.ToLower(query)
	results := make([]SearchResult, 0, limit)
	for i := len(records) - 1; i >= 0 && len(results) < limit; i-- {
		record := records[i]
		haystack := strings.ToLower(string(record.Type) + " " + string(record.Payload))
		if !strings.Contains(haystack, query) {
			continue
		}
		results = append(results, SearchResult{
			Kind:       "event",
			ID:         record.ID,
			Key:        fmt.Sprintf("event-%d", record.ID),
			ProjectKey: projectKey,
			Title:      string(record.Type),
			Status:     string(record.StreamType),
			Summary:    truncate(string(record.Payload), 240),
			OccurredAt: record.OccurredAt,
			Source:     "runtime.events",
		})
	}
	return results, nil
}

func (service Service) runsForTask(ctx context.Context, taskID int64, limit int) ([]sqlite.Run, error) {
	rows, err := service.Store.DB().QueryContext(ctx, `
		SELECT id, task_id, executor, status, attempt, started_at, finished_at, summary, terminal_reason, artifacts_json
		FROM runs
		WHERE task_id = ?
		ORDER BY id ASC
		LIMIT ?
	`, taskID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var runs []sqlite.Run
	for rows.Next() {
		var run sqlite.Run
		var startedAt string
		var finishedAt sql.NullString
		if err := rows.Scan(&run.ID, &run.TaskID, &run.Executor, &run.Status, &run.Attempt, &startedAt, &finishedAt, &run.Summary, &run.TerminalReason, &run.ArtifactsJSON); err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

func newRunContexts(runs []sqlite.Run) []RunContext {
	contexts := make([]RunContext, 0, len(runs))
	for _, run := range runs {
		contexts = append(contexts, RunContext{
			ID:             run.ID,
			Status:         run.Status,
			Executor:       run.Executor,
			Attempt:        run.Attempt,
			Summary:        run.Summary,
			TerminalReason: run.TerminalReason,
		})
	}
	return contexts
}

func newEventContexts(records []runtimeevents.Record) []EventContext {
	contexts := make([]EventContext, 0, len(records))
	for _, record := range records {
		contexts = append(contexts, EventContext{
			ID:         record.ID,
			Type:       string(record.Type),
			Scope:      record.Scope,
			Payload:    record.Payload,
			OccurredAt: record.OccurredAt,
		})
	}
	return contexts
}

func like(value string) string {
	return "%" + strings.ToLower(strings.TrimSpace(value)) + "%"
}

func truncate(value string, max int) string {
	if len(value) <= max {
		return value
	}
	return value[:max]
}
