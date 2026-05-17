package tui

type Model struct {
	Name                    string
	TelemetryAvailable      bool
	TelemetryUnavailable    string
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
	Flows                   []FlowRow
	Goals                   []GoalRow
	Schedules               []ScheduleRoutineRow
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

type FlowRow struct {
	Direction string
	Ref       string
	Source    string
	Status    string
	Subject   string
}

type GoalRow struct {
	ID         int64
	Title      string
	Status     string
	CurrentRun string
}

type ScheduleRoutineRow struct {
	Source         string
	Key            string
	Project        string
	Status         string
	DueStatus      string
	NextDueAt      string
	LastRanAt      string
	LastWorkItem   string
	LastWorkStatus string
	LastWorkDetail string
	LastWorkReview string
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
