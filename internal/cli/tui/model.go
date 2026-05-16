package tui

type Model struct {
	Name                    string
	TelemetryAvailable      bool
	Status                  string
	HealthScore             int
	TelemetryStale          bool
	LifecyclePhase          string
	ActiveRuns              int
	BlockedItems            int
	ApprovalsWaiting        int
	ReviewQueueItems        int
	FailedWorkItems         int
	RecoveryRecommendations int
	Agents                  []AgentRow
	Goals                   []GoalRow
	PullRequests            []PullRequestRow
	Approvals               []ApprovalRow
	Logs                    []LogEntry
	LogsUnavailable         string
}

type AgentRow struct {
	Name    string
	Task    string
	Project string
	Status  string
}

type GoalRow struct {
	ID         int64
	Title      string
	Status     string
	CurrentRun string
}

type PullRequestRow struct {
	Project string
	Repo    string
	Number  int
	Title   string
	State   string
	CI      string
	URL     string
}

type ApprovalRow struct {
	ID       int64
	Task     string
	Project  string
	Status   string
	Resolver string
}

type LogEntry struct {
	Timestamp string
	Line      string
	Labels    map[string]string
}
