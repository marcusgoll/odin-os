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
	Playbook    string
}

type Config struct {
	QueuePressureThreshold      int
	ExecutorFreshnessTTL        time.Duration
	ProjectionFreshnessTTL      time.Duration
	SourceFreshnessTTL          time.Duration
	RepeatedRunFailureThreshold int
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
