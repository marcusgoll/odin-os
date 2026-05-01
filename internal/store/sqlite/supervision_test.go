package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSupervisionControlPersistsKillSwitchAndConfigHash(t *testing.T) {
	ctx := context.Background()
	store := openMigratedTestStore(t, "supervision-control.db")
	defer store.Close()

	recorded, err := store.UpsertSupervisionControl(ctx, UpsertSupervisionControlParams{
		ModeKey:              "stage7_supervised_agency",
		Status:               "enabled",
		KillSwitchActive:     true,
		ConfigHash:           "sha256:config-a",
		MaxConcurrentTasks:   1,
		DryRun:               false,
		RequireHumanApproval: true,
		UpdatedBy:            "operator",
	})
	if err != nil {
		t.Fatalf("UpsertSupervisionControl() error = %v", err)
	}

	got, err := store.GetSupervisionControl(ctx, "stage7_supervised_agency")
	if err != nil {
		t.Fatalf("GetSupervisionControl() error = %v", err)
	}

	if got.ID != recorded.ID {
		t.Fatalf("GetSupervisionControl().ID = %d, want %d", got.ID, recorded.ID)
	}
	if !got.KillSwitchActive {
		t.Fatalf("GetSupervisionControl().KillSwitchActive = false, want true")
	}
	if got.ConfigHash != "sha256:config-a" {
		t.Fatalf("GetSupervisionControl().ConfigHash = %q, want sha256:config-a", got.ConfigHash)
	}
	if got.MaxConcurrentTasks != 1 || got.DryRun || !got.RequireHumanApproval {
		t.Fatalf("GetSupervisionControl() defaults = max=%d dry_run=%v approval=%v, want Stage 7 guarded state", got.MaxConcurrentTasks, got.DryRun, got.RequireHumanApproval)
	}

	updated, err := store.UpsertSupervisionControl(ctx, UpsertSupervisionControlParams{
		ModeKey:              "stage7_supervised_agency",
		Status:               "enabled",
		KillSwitchActive:     false,
		ConfigHash:           "sha256:config-b",
		MaxConcurrentTasks:   1,
		DryRun:               false,
		RequireHumanApproval: true,
		UpdatedBy:            "operator",
	})
	if err != nil {
		t.Fatalf("UpsertSupervisionControl(update) error = %v", err)
	}
	if updated.ID != recorded.ID {
		t.Fatalf("updated.ID = %d, want one current control row %d", updated.ID, recorded.ID)
	}
	if updated.KillSwitchActive || updated.ConfigHash != "sha256:config-b" {
		t.Fatalf("updated control = %+v, want runtime state to outrank prior defaults", updated)
	}
}

func TestSupervisionQueueDecisionIsIdempotentForIssueSource(t *testing.T) {
	ctx := context.Background()
	store := openMigratedTestStore(t, "supervision-queue-decision.db")
	defer store.Close()
	project := createSupervisionProject(t, ctx, store)

	first, err := store.UpsertSupervisionQueueDecision(ctx, UpsertSupervisionQueueDecisionParams{
		ProjectID:    project.ID,
		Repo:         "marcusgoll/odin-os",
		IssueNumber:  77,
		Decision:     "eligible",
		Reason:       "labels_and_scope_passed",
		ConfigHash:   "sha256:config-a",
		DecisionJSON: `{"labels":["odin:ready","safety:low-risk"]}`,
	})
	if err != nil {
		t.Fatalf("UpsertSupervisionQueueDecision(first) error = %v", err)
	}

	second, err := store.UpsertSupervisionQueueDecision(ctx, UpsertSupervisionQueueDecisionParams{
		ProjectID:    project.ID,
		Repo:         "marcusgoll/odin-os",
		IssueNumber:  77,
		Decision:     "refused",
		Reason:       "forbidden_path",
		ConfigHash:   "sha256:config-b",
		DecisionJSON: `{"path":"internal/runtime/runner"}`,
	})
	if err != nil {
		t.Fatalf("UpsertSupervisionQueueDecision(second) error = %v", err)
	}

	if second.ID != first.ID {
		t.Fatalf("second.ID = %d, want idempotent issue-source decision ID %d", second.ID, first.ID)
	}
	if second.Decision != "refused" || second.Reason != "forbidden_path" || second.ConfigHash != "sha256:config-b" {
		t.Fatalf("updated decision = %+v, want latest durable scheduler decision", second)
	}

	decisions, err := store.ListSupervisionQueueDecisions(ctx, ListSupervisionQueueDecisionsParams{
		ProjectID: &project.ID,
		Repo:      "marcusgoll/odin-os",
	})
	if err != nil {
		t.Fatalf("ListSupervisionQueueDecisions() error = %v", err)
	}
	if len(decisions) != 1 {
		t.Fatalf("ListSupervisionQueueDecisions() len = %d, want 1: %+v", len(decisions), decisions)
	}
}

func TestSupervisionDispatchClaimPreventsDuplicateActiveClaim(t *testing.T) {
	ctx := context.Background()
	store := openMigratedTestStore(t, "supervision-dispatch-claim.db")
	defer store.Close()
	project := createSupervisionProject(t, ctx, store)

	first, err := store.UpsertSupervisionDispatchClaim(ctx, UpsertSupervisionDispatchClaimParams{
		ProjectID:   project.ID,
		Repo:        "marcusgoll/odin-os",
		IssueNumber: 88,
		ClaimKey:    "stage7:odin-os:88:a",
		Status:      "reserved",
		ConfigHash:  "sha256:config-a",
		ClaimedBy:   "supervision-service",
	})
	if err != nil {
		t.Fatalf("UpsertSupervisionDispatchClaim(first) error = %v", err)
	}

	_, err = store.UpsertSupervisionDispatchClaim(ctx, UpsertSupervisionDispatchClaimParams{
		ProjectID:   project.ID,
		Repo:        "marcusgoll/odin-os",
		IssueNumber: 88,
		ClaimKey:    "stage7:odin-os:88:b",
		Status:      "active",
		ConfigHash:  "sha256:config-a",
		ClaimedBy:   "supervision-service",
	})
	if err == nil {
		t.Fatalf("UpsertSupervisionDispatchClaim(duplicate active) error = nil, want active claim conflict")
	}
	if !errors.Is(err, ErrSupervisionDispatchClaimConflict) {
		t.Fatalf("UpsertSupervisionDispatchClaim(duplicate active) error = %v, want ErrSupervisionDispatchClaimConflict", err)
	}

	claims, err := store.ListSupervisionDispatchClaims(ctx, ListSupervisionDispatchClaimsParams{
		ProjectID: &project.ID,
		Repo:      "marcusgoll/odin-os",
	})
	if err != nil {
		t.Fatalf("ListSupervisionDispatchClaims() error = %v", err)
	}
	if len(claims) != 1 || claims[0].ID != first.ID || claims[0].Status != "reserved" {
		t.Fatalf("claims = %+v, want only first reserved claim", claims)
	}
}

func TestSupervisionDispatchClaimPreventsSecondGlobalActiveClaim(t *testing.T) {
	ctx := context.Background()
	store := openMigratedTestStore(t, "supervision-dispatch-claim-global.db")
	defer store.Close()
	project := createSupervisionProject(t, ctx, store)

	first, err := store.UpsertSupervisionDispatchClaim(ctx, UpsertSupervisionDispatchClaimParams{
		ProjectID:   project.ID,
		Repo:        "marcusgoll/odin-os",
		IssueNumber: 92,
		ClaimKey:    "stage7:odin-os:92:a",
		Status:      "reserved",
		ConfigHash:  "sha256:config-a",
		ClaimedBy:   "supervision-service",
	})
	if err != nil {
		t.Fatalf("UpsertSupervisionDispatchClaim(first) error = %v", err)
	}

	_, err = store.UpsertSupervisionDispatchClaim(ctx, UpsertSupervisionDispatchClaimParams{
		ProjectID:   project.ID,
		Repo:        "marcusgoll/odin-os",
		IssueNumber: 93,
		ClaimKey:    "stage7:odin-os:93:a",
		Status:      "reserved",
		ConfigHash:  "sha256:config-a",
		ClaimedBy:   "supervision-service",
	})
	if err == nil {
		t.Fatalf("UpsertSupervisionDispatchClaim(second global active) error = nil, want active claim conflict")
	}
	if !errors.Is(err, ErrSupervisionDispatchClaimConflict) {
		t.Fatalf("UpsertSupervisionDispatchClaim(second global active) error = %v, want ErrSupervisionDispatchClaimConflict", err)
	}

	claims, err := store.ListSupervisionDispatchClaims(ctx, ListSupervisionDispatchClaimsParams{})
	if err != nil {
		t.Fatalf("ListSupervisionDispatchClaims() error = %v", err)
	}
	if len(claims) != 1 || claims[0].ID != first.ID {
		t.Fatalf("claims = %+v, want only first global active claim", claims)
	}
}

func TestSupervisionDispatchClaimRetryPreservesOriginalClaimedAt(t *testing.T) {
	ctx := context.Background()
	store := openMigratedTestStore(t, "supervision-dispatch-claim-retry.db")
	defer store.Close()
	project := createSupervisionProject(t, ctx, store)

	firstTime := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	store.Now = func() time.Time { return firstTime }
	first, err := store.UpsertSupervisionDispatchClaim(ctx, UpsertSupervisionDispatchClaimParams{
		ProjectID:   project.ID,
		Repo:        "marcusgoll/odin-os",
		IssueNumber: 89,
		ClaimKey:    "stage7:odin-os:89:a",
		Status:      "reserved",
		ConfigHash:  "sha256:config-a",
		ClaimedBy:   "supervision-service",
	})
	if err != nil {
		t.Fatalf("UpsertSupervisionDispatchClaim(first) error = %v", err)
	}

	store.Now = func() time.Time { return firstTime.Add(10 * time.Minute) }
	retried, err := store.UpsertSupervisionDispatchClaim(ctx, UpsertSupervisionDispatchClaimParams{
		ProjectID:   project.ID,
		Repo:        "marcusgoll/odin-os",
		IssueNumber: 89,
		ClaimKey:    "stage7:odin-os:89:a",
		Status:      "reserved",
		ConfigHash:  "sha256:config-b",
		ClaimedBy:   "supervision-service",
	})
	if err != nil {
		t.Fatalf("UpsertSupervisionDispatchClaim(retry) error = %v", err)
	}

	if retried.ID != first.ID {
		t.Fatalf("retried.ID = %d, want same claim ID %d", retried.ID, first.ID)
	}
	if !first.Created || retried.Created {
		t.Fatalf("claim Created flags = first %t retried %t, want true then false", first.Created, retried.Created)
	}
	if !retried.ClaimedAt.Equal(first.ClaimedAt) {
		t.Fatalf("retried.ClaimedAt = %s, want original claimed_at %s", retried.ClaimedAt, first.ClaimedAt)
	}
	if !retried.UpdatedAt.Equal(firstTime.Add(10 * time.Minute)) {
		t.Fatalf("retried.UpdatedAt = %s, want retry update time", retried.UpdatedAt)
	}
}

func TestSupervisionDispatchClaimRetryPreservesActiveStatus(t *testing.T) {
	ctx := context.Background()
	store := openMigratedTestStore(t, "supervision-dispatch-claim-active-retry.db")
	defer store.Close()
	project := createSupervisionProject(t, ctx, store)

	first, err := store.UpsertSupervisionDispatchClaim(ctx, UpsertSupervisionDispatchClaimParams{
		ProjectID:   project.ID,
		Repo:        "marcusgoll/odin-os",
		IssueNumber: 90,
		ClaimKey:    "stage7:odin-os:90:a",
		Status:      "active",
		ConfigHash:  "sha256:config-a",
		ClaimedBy:   "supervision-service",
	})
	if err != nil {
		t.Fatalf("UpsertSupervisionDispatchClaim(first active) error = %v", err)
	}

	retried, err := store.UpsertSupervisionDispatchClaim(ctx, UpsertSupervisionDispatchClaimParams{
		ProjectID:   project.ID,
		Repo:        "marcusgoll/odin-os",
		IssueNumber: 90,
		ClaimKey:    "stage7:odin-os:90:a",
		Status:      "reserved",
		ConfigHash:  "sha256:config-b",
		ClaimedBy:   "supervision-service",
	})
	if err != nil {
		t.Fatalf("UpsertSupervisionDispatchClaim(retry reserved) error = %v", err)
	}

	if retried.ID != first.ID {
		t.Fatalf("retried.ID = %d, want same active claim ID %d", retried.ID, first.ID)
	}
	if !first.Created || retried.Created {
		t.Fatalf("claim Created flags = first %t retried %t, want true then false", first.Created, retried.Created)
	}
	if retried.Status != "active" {
		t.Fatalf("retried.Status = %q, want active claim preserved", retried.Status)
	}
}

func TestSupervisionDispatchClaimReleaseSetsReleasedAt(t *testing.T) {
	ctx := context.Background()
	store := openMigratedTestStore(t, "supervision-dispatch-claim-release.db")
	defer store.Close()
	project := createSupervisionProject(t, ctx, store)

	claimedAt := time.Date(2026, 5, 1, 11, 0, 0, 0, time.UTC)
	store.Now = func() time.Time { return claimedAt }
	claim, err := store.UpsertSupervisionDispatchClaim(ctx, UpsertSupervisionDispatchClaimParams{
		ProjectID:   project.ID,
		Repo:        "marcusgoll/odin-os",
		IssueNumber: 90,
		ClaimKey:    "stage7:odin-os:90:a",
		Status:      "reserved",
		ConfigHash:  "sha256:config-a",
		ClaimedBy:   "supervision-service",
	})
	if err != nil {
		t.Fatalf("UpsertSupervisionDispatchClaim() error = %v", err)
	}

	releasedAt := claimedAt.Add(30 * time.Minute)
	store.Now = func() time.Time { return releasedAt }
	released, err := store.ReleaseSupervisionDispatchClaim(ctx, ReleaseSupervisionDispatchClaimParams{
		ClaimKey: claim.ClaimKey,
		Status:   "released",
	})
	if err != nil {
		t.Fatalf("ReleaseSupervisionDispatchClaim() error = %v", err)
	}

	if released.Status != "released" {
		t.Fatalf("released.Status = %q, want released", released.Status)
	}
	if released.ReleasedAt == nil {
		t.Fatalf("released.ReleasedAt = nil, want release timestamp")
	}
	if !released.ReleasedAt.Equal(releasedAt) {
		t.Fatalf("released.ReleasedAt = %s, want %s", *released.ReleasedAt, releasedAt)
	}
	if !released.ClaimedAt.Equal(claimedAt) {
		t.Fatalf("released.ClaimedAt = %s, want original claim time %s", released.ClaimedAt, claimedAt)
	}
}

func TestSupervisionDispatchClaimReleasedClaimCannotBeResurrected(t *testing.T) {
	ctx := context.Background()
	store := openMigratedTestStore(t, "supervision-dispatch-claim-terminal-release.db")
	defer store.Close()
	project := createSupervisionProject(t, ctx, store)

	claim, err := store.UpsertSupervisionDispatchClaim(ctx, UpsertSupervisionDispatchClaimParams{
		ProjectID:   project.ID,
		Repo:        "marcusgoll/odin-os",
		IssueNumber: 91,
		ClaimKey:    "stage7:odin-os:91:a",
		Status:      "reserved",
		ConfigHash:  "sha256:config-a",
		ClaimedBy:   "supervision-service",
	})
	if err != nil {
		t.Fatalf("UpsertSupervisionDispatchClaim() error = %v", err)
	}
	released, err := store.ReleaseSupervisionDispatchClaim(ctx, ReleaseSupervisionDispatchClaimParams{
		ClaimKey: claim.ClaimKey,
		Status:   "released",
	})
	if err != nil {
		t.Fatalf("ReleaseSupervisionDispatchClaim() error = %v", err)
	}

	_, err = store.UpsertSupervisionDispatchClaim(ctx, UpsertSupervisionDispatchClaimParams{
		ProjectID:   project.ID,
		Repo:        "marcusgoll/odin-os",
		IssueNumber: 91,
		ClaimKey:    "stage7:odin-os:91:a",
		Status:      "reserved",
		ConfigHash:  "sha256:config-b",
		ClaimedBy:   "supervision-service",
	})
	if err == nil {
		t.Fatalf("UpsertSupervisionDispatchClaim(after release) error = nil, want terminal release error")
	}
	if !errors.Is(err, ErrSupervisionDispatchClaimReleased) {
		t.Fatalf("UpsertSupervisionDispatchClaim(after release) error = %v, want ErrSupervisionDispatchClaimReleased", err)
	}

	claims, err := store.ListSupervisionDispatchClaims(ctx, ListSupervisionDispatchClaimsParams{
		ProjectID: &project.ID,
		Repo:      "marcusgoll/odin-os",
	})
	if err != nil {
		t.Fatalf("ListSupervisionDispatchClaims() error = %v", err)
	}
	if len(claims) != 1 {
		t.Fatalf("ListSupervisionDispatchClaims() len = %d, want 1", len(claims))
	}
	if claims[0].Status != "released" || claims[0].ReleasedAt == nil {
		t.Fatalf("claim after rejected resurrection = %+v, want released terminal claim", claims[0])
	}
	if !claims[0].ReleasedAt.Equal(*released.ReleasedAt) {
		t.Fatalf("claim ReleasedAt = %v, want original release time %v", claims[0].ReleasedAt, released.ReleasedAt)
	}
}

func TestSupervisionRecoveryObservationRecordsBlockedRestart(t *testing.T) {
	ctx := context.Background()
	store := openMigratedTestStore(t, "supervision-recovery-observation.db")
	defer store.Close()
	project := createSupervisionProject(t, ctx, store)

	observed, err := store.CreateSupervisionRecoveryObservation(ctx, CreateSupervisionRecoveryObservationParams{
		ProjectID:       &project.ID,
		ModeKey:         "stage7_supervised_agency",
		ObservationType: "restart_recovery",
		Status:          "blocked",
		Reason:          "active_claim_config_hash_mismatch",
		ConfigHash:      "sha256:config-b",
		DetailsJSON:     `{"claim_config_hash":"sha256:config-a"}`,
	})
	if err != nil {
		t.Fatalf("CreateSupervisionRecoveryObservation() error = %v", err)
	}

	observations, err := store.ListSupervisionRecoveryObservations(ctx, ListSupervisionRecoveryObservationsParams{
		ProjectID: &project.ID,
		ModeKey:   "stage7_supervised_agency",
		Limit:     1,
	})
	if err != nil {
		t.Fatalf("ListSupervisionRecoveryObservations() error = %v", err)
	}
	if len(observations) != 1 {
		t.Fatalf("ListSupervisionRecoveryObservations() len = %d, want 1", len(observations))
	}
	if observations[0].ID != observed.ID || observations[0].Status != "blocked" || observations[0].Reason != "active_claim_config_hash_mismatch" {
		t.Fatalf("observation = %+v, want latest blocked restart observation", observations[0])
	}
}

func createSupervisionProject(t *testing.T, ctx context.Context, store *Store) Project {
	t.Helper()

	project, err := store.CreateProject(ctx, CreateProjectParams{
		Key:           "odin-core",
		Name:          "Odin Core",
		Scope:         "project",
		GitRoot:       "/tmp/odin-os",
		DefaultBranch: "main",
		GitHubRepo:    "marcusgoll/odin-os",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	return project
}
