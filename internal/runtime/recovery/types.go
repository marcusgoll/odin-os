package recovery

import (
	"database/sql"
	"time"
)

type FaultKey string

const (
	FaultExecutorHealthStale  FaultKey = "executor_health_stale"
	FaultProjectionStale      FaultKey = "projection_stale"
	FaultSourceFreshnessStale FaultKey = "source_freshness_stale"
	FaultQueuePressureHigh    FaultKey = "queue_pressure_high"
	FaultRunFailureRepeated   FaultKey = "run_failure_repeated"
	FaultWakePacketInvalid    FaultKey = "wake_packet_invalid"
)

type DecisionMode string

const (
	DecisionModeIgnore           DecisionMode = "ignore"
	DecisionModeIncidentOnly     DecisionMode = "incident_only"
	DecisionModePlaybook         DecisionMode = "playbook"
	DecisionModeApprovalRequired DecisionMode = "approval_required"
	DecisionModeEscalate         DecisionMode = "escalate"
)

type OutcomeStatus string

const (
	OutcomeStatusIncidentOnly OutcomeStatus = "incident_only"
	OutcomeStatusCompleted    OutcomeStatus = "completed"
	OutcomeStatusFailed       OutcomeStatus = "failed"
	OutcomeStatusSuppressed   OutcomeStatus = "suppressed"
	OutcomeStatusEscalated    OutcomeStatus = "escalated"
)

type ActionResultStatus string

const (
	ActionResultStatusCompleted ActionResultStatus = "completed"
	ActionResultStatusFailed    ActionResultStatus = "failed"
	ActionResultStatusEscalated ActionResultStatus = "escalated"
)

type Observation struct {
	FaultKey   FaultKey
	SubjectKey string
	Scope      string
	Severity   string
	Summary    string
	ProjectID  *int64
	TaskID     *int64
	RunID      *int64
}

type Decision struct {
	Observation Observation
	Mode        DecisionMode
	Playbook    string
	Reason      string
	NextAction  string
}

type Config struct {
	QueuePressureThreshold      int
	ExecutorFreshnessTTL        time.Duration
	ProjectionFreshnessTTL      time.Duration
	SourceFreshnessTTL          time.Duration
	RepeatedRunFailureThreshold int
	ExecutorKeys                []string
}

type Monitor struct {
	DB     *sql.DB
	Config Config
	Now    func() time.Time
}

func DefaultConfig() Config {
	return Config{
		QueuePressureThreshold:      10,
		ExecutorFreshnessTTL:        30 * time.Minute,
		ProjectionFreshnessTTL:      30 * time.Minute,
		SourceFreshnessTTL:          30 * time.Minute,
		RepeatedRunFailureThreshold: 2,
	}
}

func isZeroConfig(config Config) bool {
	return config.QueuePressureThreshold == 0 &&
		config.ExecutorFreshnessTTL == 0 &&
		config.ProjectionFreshnessTTL == 0 &&
		config.SourceFreshnessTTL == 0 &&
		config.RepeatedRunFailureThreshold == 0 &&
		len(config.ExecutorKeys) == 0
}

func applyDefaults(config Config) Config {
	defaults := DefaultConfig()
	if config.QueuePressureThreshold == 0 {
		config.QueuePressureThreshold = defaults.QueuePressureThreshold
	}
	if config.ExecutorFreshnessTTL == 0 {
		config.ExecutorFreshnessTTL = defaults.ExecutorFreshnessTTL
	}
	if config.ProjectionFreshnessTTL == 0 {
		config.ProjectionFreshnessTTL = defaults.ProjectionFreshnessTTL
	}
	if config.SourceFreshnessTTL == 0 {
		config.SourceFreshnessTTL = defaults.SourceFreshnessTTL
	}
	if config.RepeatedRunFailureThreshold == 0 {
		config.RepeatedRunFailureThreshold = defaults.RepeatedRunFailureThreshold
	}
	return config
}
