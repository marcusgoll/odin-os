package followups

import (
	"fmt"
	"strings"
	"time"
)

type Status string

const (
	StatusActive    Status = "active"
	StatusPaused    Status = "paused"
	StatusBlocked   Status = "blocked"
	StatusCompleted Status = "completed"
	StatusSkipped   Status = "skipped"
	StatusArchived  Status = "archived"
	StatusDue       Status = "due"
)

type CadenceMode string

const (
	CadenceModeOnce      CadenceMode = "once"
	CadenceModeRecurring CadenceMode = "recurring"
)

type CadenceInterval string

const (
	CadenceIntervalDaily     CadenceInterval = "daily"
	CadenceIntervalWeekly    CadenceInterval = "weekly"
	CadenceIntervalMonthly   CadenceInterval = "monthly"
	CadenceIntervalQuarterly CadenceInterval = "quarterly"
)

type Cadence struct {
	Mode     CadenceMode     `json:"mode"`
	Interval CadenceInterval `json:"interval,omitempty"`
}

type FollowUpObligation struct {
	ID                 int64
	WorkspaceID        int64
	InitiativeID       *int64
	CompanionID        *int64
	TargetProjectID    int64
	Title              string
	Status             Status
	Cadence            Cadence
	NextDueAt          time.Time
	LastMaterializedAt *time.Time
	LastCompletedAt    *time.Time
	PolicyJSON         string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type CreateParams struct {
	WorkspaceID     int64
	InitiativeID    *int64
	CompanionID     *int64
	TargetProjectID *int64
	Title           string
	Cadence         Cadence
	NextDueAt       time.Time
	PolicyJSON      string
}

type MaterializeParams struct {
	ObligationID int64
	TaskKey      string
	Title        string
	ActionKey    string
	Scope        string
	RequestedBy  string
}

type MaterializationResult struct {
	Obligation    FollowUpObligation
	TaskID        int64
	OccurrenceKey string
	Reused        bool
}

func (cadence Cadence) Validate() error {
	switch cadence.Mode {
	case CadenceModeOnce:
		if strings.TrimSpace(string(cadence.Interval)) != "" {
			return fmt.Errorf("one-time cadence does not accept an interval")
		}
		return nil
	case CadenceModeRecurring:
		switch cadence.Interval {
		case CadenceIntervalDaily, CadenceIntervalWeekly, CadenceIntervalMonthly, CadenceIntervalQuarterly:
			return nil
		default:
			return fmt.Errorf("unsupported recurring cadence interval %q", cadence.Interval)
		}
	default:
		return fmt.Errorf("unsupported cadence mode %q", cadence.Mode)
	}
}

func (cadence Cadence) NextDueAfter(base time.Time) (time.Time, error) {
	if err := cadence.Validate(); err != nil {
		return time.Time{}, err
	}

	base = base.UTC()
	switch cadence.Mode {
	case CadenceModeOnce:
		return base, nil
	case CadenceModeRecurring:
		switch cadence.Interval {
		case CadenceIntervalDaily:
			return base.AddDate(0, 0, 1), nil
		case CadenceIntervalWeekly:
			return base.AddDate(0, 0, 7), nil
		case CadenceIntervalMonthly:
			return base.AddDate(0, 1, 0), nil
		case CadenceIntervalQuarterly:
			return base.AddDate(0, 3, 0), nil
		default:
			return time.Time{}, fmt.Errorf("unsupported recurring cadence interval %q", cadence.Interval)
		}
	default:
		return time.Time{}, fmt.Errorf("unsupported cadence mode %q", cadence.Mode)
	}
}

func (obligation FollowUpObligation) DueStatus(now time.Time) Status {
	switch obligation.Status {
	case StatusPaused, StatusBlocked, StatusCompleted, StatusSkipped, StatusArchived:
		return obligation.Status
	}
	if !obligation.NextDueAt.After(now) {
		return StatusDue
	}
	return StatusActive
}

func (obligation FollowUpObligation) OccurrenceKey() string {
	return obligation.NextDueAt.UTC().Format(time.RFC3339Nano)
}
