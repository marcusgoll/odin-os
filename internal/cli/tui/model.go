package tui

type Model struct {
	TelemetryAvailable bool
	Status             string
	HealthScore        int
	TelemetryStale     bool
	LifecyclePhase     string
	ActiveRuns         int
	Logs               []LogEntry
	LogsUnavailable    string
}

type LogEntry struct {
	Timestamp string
	Line      string
	Labels    map[string]string
}
