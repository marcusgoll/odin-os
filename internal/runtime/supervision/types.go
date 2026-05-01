package supervision

import "time"

const (
	ModeKeyStage7SupervisedAgency = "stage7_supervised_agency"

	ControlStatusEnabled = "enabled"
	ControlStatusStopped = "stopped"

	ClaimStatusReserved = "reserved"
	ClaimStatusActive   = "active"

	DecisionEligible = "eligible"
	DecisionRefused  = "refused"

	RefusalMissingRequiredLabel = "missing_required_label"
	RefusalUnknownScope         = "unknown_scope"
	RefusalForbiddenPath        = "forbidden_path"
	RefusalSensitiveTestScope   = "sensitive_test_scope"
	RefusalKillSwitchActive     = "kill_switch_active"
	RefusalRecoveryBlocked      = "recovery_blocked"
	RefusalConcurrencyLimit     = "concurrency_limit_reached"
	RecoveryStatusClean         = "clean"
	RecoveryStatusBlocked       = "blocked"
	RecoveryReasonNoStaleClaims = "no_stale_claims"
	RecoveryObservationRestart  = "restart_recovery"
	SideEffectNotStarted        = "not_started"
	SideEffectNotCreated        = "not_created"
	SideEffectNotMerged         = "not_merged"
	supervisionServiceClaimedBy = "supervision-service"
)

type Project struct {
	ID   int64
	Key  string
	Repo string
}

type Issue struct {
	Provider     string
	Repo         string
	Number       int
	Title        string
	Body         string
	Labels       []string
	URL          string
	State        string
	PullRequest  bool
	ChangedPaths []string
}

type Eligibility struct {
	Eligible      bool     `json:"eligible"`
	RefusalReason string   `json:"refusal_reason,omitempty"`
	Labels        []string `json:"labels"`
	ChangedPaths  []string `json:"changed_paths"`
}

type SideEffects struct {
	CodexExecution string `json:"codex_execution"`
	PRs            string `json:"prs"`
	Merge          string `json:"merge"`
	Deployment     string `json:"deployment"`
}

type ControlState struct {
	Status               string `json:"status"`
	KillSwitchActive     bool   `json:"kill_switch_active"`
	ConfigHash           string `json:"config_hash"`
	MaxConcurrentTasks   int    `json:"max_concurrent_tasks"`
	DryRun               bool   `json:"dry_run"`
	RequireHumanApproval bool   `json:"require_human_approval"`
	UpdatedBy            string `json:"updated_by"`
}

type QueueDecision struct {
	ProjectKey    string    `json:"project_key"`
	Repo          string    `json:"repo"`
	IssueNumber   int       `json:"issue_number"`
	Decision      string    `json:"decision"`
	Eligible      bool      `json:"eligible"`
	RefusalReason string    `json:"refusal_reason,omitempty"`
	ClaimKey      string    `json:"claim_key,omitempty"`
	DecidedAt     time.Time `json:"decided_at,omitempty"`
}

type PlannedClaim struct {
	ProjectKey   string    `json:"project_key"`
	Repo         string    `json:"repo"`
	IssueNumber  int       `json:"issue_number"`
	ClaimKey     string    `json:"claim_key"`
	Status       string    `json:"status"`
	ClaimedAt    time.Time `json:"claimed_at,omitempty"`
	NewlyCreated bool      `json:"newly_created,omitempty"`
}

type RecoveryReport struct {
	Status       string `json:"status"`
	Reason       string `json:"reason"`
	ActiveClaims int    `json:"active_claims"`
}

type Report struct {
	ModeKey     string          `json:"mode_key"`
	Control     ControlState    `json:"control"`
	Decisions   []QueueDecision `json:"decisions,omitempty"`
	Claims      []PlannedClaim  `json:"claims,omitempty"`
	Recovery    RecoveryReport  `json:"recovery,omitempty"`
	SideEffects SideEffects     `json:"side_effects"`
}

func notStartedSideEffects() SideEffects {
	return SideEffects{
		CodexExecution: SideEffectNotStarted,
		PRs:            SideEffectNotCreated,
		Merge:          SideEffectNotMerged,
		Deployment:     SideEffectNotStarted,
	}
}
