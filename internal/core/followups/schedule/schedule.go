package schedule

import "time"

type State string

const (
	StateActive         State = "active"
	StateDue            State = "due"
	StateOverdue        State = "overdue"
	DefaultOverdueGrace       = 24 * time.Hour
)

func Classify(nextDueAt, now time.Time, overdueGrace time.Duration) State {
	if overdueGrace < 0 {
		overdueGrace = 0
	}
	if now.UTC().Sub(nextDueAt.UTC()) > overdueGrace {
		return StateOverdue
	}
	return StateDue
}

func SummaryStatus(status string, nextDueAt, now time.Time, overdueGrace time.Duration) string {
	switch status {
	case "paused", "blocked", "completed", "skipped", "archived":
		return status
	}

	now = now.UTC()
	nextDueAt = nextDueAt.UTC()
	if nextDueAt.After(now) {
		return string(StateActive)
	}
	return string(Classify(nextDueAt, now, overdueGrace))
}
