# Universal Intake V1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Harden Odin's canonical raw-to-reviewed-draft intake lane so raw inputs become Reviewable Intake Proposals without default Work Item creation, Run Attempts, dispatch, or external mutation.

**Architecture:** Reuse the existing `intake_items` SQLite authority, `odin intake raw/process/review` command family, runtime intake events, `/overview` Intake Inbox projection, and registry agents. Add a small core intake contract package for source envelopes, proposal envelopes, lifecycle aliases, and dedupe identity so adapters normalize facts while Odin core owns reviewable intake semantics.

**Tech Stack:** Go, SQLite migrations already present, Odin CLI lifecycle runner, runtime events, existing integration tests, real `odin` CLI proof.

---

## File Structure

- Create `internal/core/intake/envelope.go`: source envelope type, validation, source facts JSON conversion, and dedupe recipe constants.
- Create `internal/core/intake/envelope_test.go`: table tests for valid CLI envelope, missing fields, invalid adapter facts, and evidence references.
- Create `internal/core/intake/proposal.go`: Reviewable Intake Proposal envelope, typed draft artifact constants, lifecycle aliases, and status mapping helpers.
- Create `internal/core/intake/proposal_test.go`: tests for proposal serialization, typed artifacts, approval posture, and compatibility status aliases.
- Modify `internal/app/lifecycle/run.go`: replace local raw source fact construction and scattered proposal/status fields with the new core helpers; keep existing operator command shape.
- Modify `internal/app/lifecycle/run_test.go`: strengthen CLI E2E tests for proposal creation, negative proof, status aliases, and no default promotion.
- Modify `internal/store/sqlite/intake_items_test.go`: add store-level proof for source facts, dedupe recipe preservation, canonical duplicate link, and routing notes shape.
- Modify `internal/cli/overview/service.go`: map compatibility statuses into canonical Intake Inbox counts without losing existing output.
- Modify `internal/cli/overview/service_test.go`: assert canonical proposal/review counts and linked duplicate visibility.
- Modify `internal/runtime/events/events.go` only if proposal envelope fields need event payload additions; avoid event churn if existing payloads already carry enough evidence.
- Modify `docs/contracts/external-intake.md`: align the external contract with the source envelope and no-promotion boundary.
- Modify `docs/contracts/runtime-events.md`: document the intake-to-proposal event expectations and negative execution boundary.

## Task 1: Source Envelope Contract

**Files:**
- Create: `internal/core/intake/envelope.go`
- Create: `internal/core/intake/envelope_test.go`
- Modify: `internal/app/lifecycle/run.go`

- [ ] **Step 1: Write failing source envelope tests**

Add `internal/core/intake/envelope_test.go`:

```go
package intake

import (
	"encoding/json"
	"testing"
	"time"
)

func TestSourceEnvelopeValidatesAndBuildsFacts(t *testing.T) {
	observedAt := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	envelope := SourceEnvelope{
		SourceFamily:     "cli",
		ExternalObjectID: "manual-1",
		EventKind:        "request",
		ObservedAt:       observedAt,
		Subject:          "Build universal intake proposal",
		Body:             "Preserve raw input and prepare a reviewable proposal.",
		Actor:            "operator",
		SourceURI:        "odin://manual/intake/manual-1",
		EvidenceRefs:     []string{"stdin"},
		AdapterFacts: map[string]any{
			"cli": map[string]any{"payload_policy": "stored_in_source_facts_json"},
		},
	}

	facts, err := envelope.SourceFactsJSON()
	if err != nil {
		t.Fatalf("SourceFactsJSON() error = %v", err)
	}
	if !json.Valid([]byte(facts)) {
		t.Fatalf("facts json is invalid: %s", facts)
	}

	dedupe := envelope.DedupeKey("default")
	if dedupe == "" || dedupe == "manual-1" {
		t.Fatalf("DedupeKey() = %q, want Odin-owned derived key", dedupe)
	}
}

func TestSourceEnvelopeRejectsMissingCoreFields(t *testing.T) {
	envelope := SourceEnvelope{SourceFamily: "cli", EventKind: "request"}
	if err := envelope.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want missing subject/actor error")
	}
}

func TestSourceEnvelopeRejectsUnnamespacedAdapterFacts(t *testing.T) {
	envelope := SourceEnvelope{
		SourceFamily: "cli",
		EventKind:    "request",
		Subject:      "Build universal intake proposal",
		Actor:        "operator",
		AdapterFacts: map[string]any{"payload_policy": "stored_in_source_facts_json"},
	}
	if err := envelope.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want adapter facts namespace error")
	}
}
```

- [ ] **Step 2: Run the failing tests**

Run:

```bash
go test ./internal/core/intake -run 'TestSourceEnvelope' -count=1
```

Expected: fail because `SourceEnvelope`, `Validate`, `SourceFactsJSON`, and `DedupeKey` are not defined.

- [ ] **Step 3: Implement the source envelope**

Create `internal/core/intake/envelope.go`:

```go
package intake

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

const DedupeRecipeVersion = "odin-intake-v1"

type SourceEnvelope struct {
	SourceFamily     string         `json:"source_family"`
	ExternalObjectID string         `json:"external_object_id,omitempty"`
	EventKind        string         `json:"event_kind"`
	ObservedAt       time.Time      `json:"observed_at,omitempty"`
	Subject          string         `json:"subject"`
	Body             string         `json:"body,omitempty"`
	Summary          string         `json:"summary,omitempty"`
	Actor            string         `json:"actor"`
	SourceURI        string         `json:"source_uri,omitempty"`
	EvidenceRefs     []string       `json:"evidence_refs,omitempty"`
	AdapterFacts     map[string]any `json:"adapter_facts,omitempty"`
}

func (envelope SourceEnvelope) Validate() error {
	if strings.TrimSpace(envelope.SourceFamily) == "" {
		return fmt.Errorf("source_family is required")
	}
	if strings.TrimSpace(envelope.EventKind) == "" {
		return fmt.Errorf("event_kind is required")
	}
	if strings.TrimSpace(envelope.Subject) == "" {
		return fmt.Errorf("subject is required")
	}
	if strings.TrimSpace(envelope.Actor) == "" {
		return fmt.Errorf("actor is required")
	}
	for key := range envelope.AdapterFacts {
		if !strings.Contains(key, ".") && key != envelope.SourceFamily {
			return fmt.Errorf("adapter_facts key %q must be namespaced or match source_family", key)
		}
	}
	return nil
}

func (envelope SourceEnvelope) SourceFactsJSON() (string, error) {
	if err := envelope.Validate(); err != nil {
		return "", err
	}
	facts := map[string]any{
		"source_family":      envelope.SourceFamily,
		"external_object_id": envelope.ExternalObjectID,
		"event_kind":         envelope.EventKind,
		"subject":            envelope.Subject,
		"body":               envelope.Body,
		"summary":            envelope.Summary,
		"actor":              envelope.Actor,
		"source_uri":         envelope.SourceURI,
		"evidence_refs":      envelope.EvidenceRefs,
		"adapter_facts":      envelope.AdapterFacts,
	}
	if !envelope.ObservedAt.IsZero() {
		facts["observed_at"] = envelope.ObservedAt.UTC().Format(time.RFC3339)
	}
	encoded, err := json.Marshal(facts)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func (envelope SourceEnvelope) DedupeKey(workspaceID string) string {
	parts := []string{
		strings.ToLower(strings.TrimSpace(workspaceID)),
		strings.ToLower(strings.TrimSpace(envelope.SourceFamily)),
		strings.ToLower(strings.TrimSpace(envelope.EventKind)),
		normalizedFingerprint(envelope.ExternalObjectID),
		normalizedFingerprint(envelope.Subject),
		normalizedFingerprint(envelope.SourceURI),
	}
	if envelope.ExternalObjectID == "" && envelope.SourceURI == "" {
		parts = append(parts, normalizedFingerprint(envelope.Body), normalizedFingerprint(envelope.Summary))
	}
	sort.Strings(parts[3:])
	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return "odin-intake:" + hex.EncodeToString(sum[:])[:24]
}

func normalizedFingerprint(value string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(value))), " ")
}
```

- [ ] **Step 4: Wire raw CLI creation through the envelope**

In `internal/app/lifecycle/run.go`, change `runRawIntakeCreate` so it builds `intake.SourceEnvelope` before calling `CreateIntakeItem`.

Use this shape:

```go
envelope := intake.SourceEnvelope{
	SourceFamily:     command.Source,
	ExternalObjectID: command.ActionKey,
	EventKind:        command.Type,
	Subject:          command.Title,
	Summary:          command.Title,
	Actor:            command.RequestedBy,
	AdapterFacts: map[string]any{
		command.Source: map[string]any{
			"payload_policy": rawIntakePayloadPolicy,
			"payload":        json.RawMessage(payloadJSON),
		},
	},
}
sourceFactsJSON, err := envelope.SourceFactsJSON()
if err != nil {
	return err
}
dedupeKey := command.DedupKey
if strings.TrimSpace(dedupeKey) == "" {
	dedupeKey = envelope.DedupeKey(workspaces.DefaultWorkspaceKey)
}
```

Then pass:

```go
DedupeKey:           dedupeKey,
DedupeRecipeVersion: intake.DedupeRecipeVersion,
SourceFactsJSON:     sourceFactsJSON,
```

Keep compatibility with explicit `--dedup-key`; do not remove existing parser validation in this task.

- [ ] **Step 5: Run tests**

Run:

```bash
go test ./internal/core/intake ./internal/app/lifecycle -run 'TestSourceEnvelope|TestRunRawIntake' -count=1
```

Expected: pass.

- [ ] **Step 6: Commit**

```bash
git add internal/core/intake/envelope.go internal/core/intake/envelope_test.go internal/app/lifecycle/run.go
git commit -m "feat: centralize intake source envelope"
```

## Task 2: Reviewable Intake Proposal Envelope

**Files:**
- Create: `internal/core/intake/proposal.go`
- Create: `internal/core/intake/proposal_test.go`
- Modify: `internal/app/lifecycle/run.go`

- [ ] **Step 1: Write failing proposal tests**

Add `internal/core/intake/proposal_test.go`:

```go
package intake

import "testing"

func TestReviewableProposalPreservesTypedDraftArtifact(t *testing.T) {
	proposal := ReviewableProposal{
		SourceIntakeKey:       "intake-7",
		Title:                 "Investigate import incident",
		Category:              "bug",
		Route:                 "draft_incident_review",
		Summary:               "Prepare incident review for operator.",
		DraftArtifact:         DraftArtifact{Kind: DraftIncidentReview, Title: "Investigate import incident"},
		RiskLevel:             RiskMedium,
		ApprovalPosture:       ApprovalNeedsReview,
		DedupeResult:          "unique",
		RecommendedNextAction: "review",
		OperatorNextAction:    "odin intake review show intake-7",
	}
	if err := proposal.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if proposal.DraftArtifact.Kind != DraftIncidentReview {
		t.Fatalf("DraftArtifact.Kind = %q, want %q", proposal.DraftArtifact.Kind, DraftIncidentReview)
	}
}

func TestLifecycleAliasMapsCompatibilityStatuses(t *testing.T) {
	cases := map[string]string{
		"received":                       StateReceived,
		"review_required":                StateReviewRequired,
		"needs_clarification":            StateNeedsClarification,
		"duplicate_linked_or_suppressed": StateDuplicateLinked,
		"approval_required":              StateReviewRequired,
		"accepted":                       StateAcceptedForPromotion,
		"archived":                       StateArchived,
	}
	for input, want := range cases {
		if got := CanonicalState(input); got != want {
			t.Fatalf("CanonicalState(%q) = %q, want %q", input, got, want)
		}
	}
}
```

- [ ] **Step 2: Run failing tests**

Run:

```bash
go test ./internal/core/intake -run 'TestReviewableProposal|TestLifecycleAlias' -count=1
```

Expected: fail because proposal types and constants are not defined.

- [ ] **Step 3: Implement proposal and lifecycle helpers**

Create `internal/core/intake/proposal.go`:

```go
package intake

import (
	"fmt"
	"strings"
)

const (
	StateReceived             = "received"
	StateProcessing           = "processing"
	StateReviewRequired       = "review_required"
	StateNeedsClarification   = "needs_clarification"
	StateDuplicateLinked      = "duplicate_linked"
	StateArchived             = "archived"
	StateAcceptedForPromotion = "accepted_for_promotion"
	StateErrored              = "errored"
)

const (
	DraftTask              = "draft_task"
	DraftResearch          = "draft_research"
	DraftDocument          = "draft_document"
	DraftIncidentReview    = "draft_incident_review"
	DraftRoutine           = "draft_routine"
	DraftFollowUp          = "draft_follow_up"
	DraftPolicyChange      = "draft_policy_change"
	DraftDestructiveAction = "draft_destructive_action"
	ArchiveCandidate       = "archive_candidate"
)

const (
	RiskLow    = "low"
	RiskMedium = "medium"
	RiskHigh   = "high"
)

const (
	ApprovalNeedsReview     = "needs_review"
	ApprovalRequired        = "approval_required"
	ApprovalBlocked         = "blocked"
	ApprovalReadyToPromote  = "ready_to_promote"
	ApprovalNoWorkNecessary = "no_work_necessary"
)

type ReviewableProposal struct {
	SourceIntakeKey       string        `json:"source_intake_key"`
	Title                 string        `json:"title"`
	Category              string        `json:"category"`
	Route                 string        `json:"route"`
	Summary               string        `json:"summary"`
	DraftArtifact         DraftArtifact `json:"draft_artifact"`
	AcceptanceCriteria    []string      `json:"acceptance_criteria,omitempty"`
	ClarificationPrompts  []string      `json:"clarification_prompts,omitempty"`
	RiskLevel             string        `json:"risk_level"`
	ApprovalPosture       string        `json:"approval_posture"`
	MissingConstraints    []string      `json:"missing_constraints,omitempty"`
	DedupeResult          string        `json:"dedupe_result"`
	RecommendedNextAction string        `json:"recommended_next_action"`
	OperatorNextAction    string        `json:"operator_next_action"`
}

type DraftArtifact struct {
	Kind                  string `json:"kind"`
	Title                 string `json:"title"`
	ReviewState           string `json:"review_state"`
	ExecutionIntent       string `json:"execution_intent"`
	ExecutionIntentSource string `json:"execution_intent_source"`
}

func (proposal ReviewableProposal) Validate() error {
	if strings.TrimSpace(proposal.SourceIntakeKey) == "" {
		return fmt.Errorf("source_intake_key is required")
	}
	if strings.TrimSpace(proposal.Title) == "" {
		return fmt.Errorf("title is required")
	}
	if strings.TrimSpace(proposal.Route) == "" {
		return fmt.Errorf("route is required")
	}
	if strings.TrimSpace(proposal.DraftArtifact.Kind) == "" {
		return fmt.Errorf("draft_artifact.kind is required")
	}
	if strings.TrimSpace(proposal.ApprovalPosture) == "" {
		return fmt.Errorf("approval_posture is required")
	}
	if strings.TrimSpace(proposal.OperatorNextAction) == "" {
		return fmt.Errorf("operator_next_action is required")
	}
	return nil
}

func CanonicalState(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "", StateReceived:
		return StateReceived
	case StateProcessing, "triaging":
		return StateProcessing
	case StateReviewRequired, "approval_required":
		return StateReviewRequired
	case StateNeedsClarification:
		return StateNeedsClarification
	case StateDuplicateLinked, "duplicate_linked_or_suppressed", "suppressed":
		return StateDuplicateLinked
	case StateArchived, "rejected", "approval_denied":
		return StateArchived
	case StateAcceptedForPromotion, "accepted":
		return StateAcceptedForPromotion
	case StateErrored, "error":
		return StateErrored
	default:
		return strings.ToLower(strings.TrimSpace(status))
	}
}
```

- [ ] **Step 4: Embed proposals in processing notes**

In `internal/app/lifecycle/run.go`, import `internal/core/intake` and add this field to `intakeProcessingNotes`:

```go
Proposal *intake.ReviewableProposal `json:"proposal,omitempty"`
```

When `buildIntakeProcessOutcome` creates `notes.DraftArtifact`, also set `notes.Proposal`:

```go
proposal := intake.ReviewableProposal{
	SourceIntakeKey:       rawIntakeKey(item.ID),
	Title:                 item.Subject,
	Category:              notes.Classification.Category,
	Route:                 route.RoutingOutcome,
	Summary:               route.DraftArtifactKind + " prepared for human review; no work item created",
	DraftArtifact: intake.DraftArtifact{
		Kind:                  route.DraftArtifactKind,
		Title:                 item.Subject,
		ReviewState:           "review_required",
		ExecutionIntent:       route.ExecutionIntent,
		ExecutionIntentSource: route.ExecutionIntentSource,
	},
	RiskLevel:             "low",
	ApprovalPosture:       intake.ApprovalNeedsReview,
	DedupeResult:          notes.Dedupe.Result,
	RecommendedNextAction: "review",
	OperatorNextAction:    "odin intake review show " + rawIntakeKey(item.ID),
}
if route.ExecutionIntent == "governance" || route.ExecutionIntent == "destructive" {
	proposal.RiskLevel = "high"
	proposal.ApprovalPosture = intake.ApprovalRequired
}
notes.Proposal = &proposal
```

For clarification outcomes, set a proposal with `ClarificationPrompts` and `ApprovalPosture: intake.ApprovalBlocked`.

- [ ] **Step 5: Run tests**

Run:

```bash
go test ./internal/core/intake ./internal/app/lifecycle -run 'TestReviewableProposal|TestLifecycleAlias|TestRunIntakeProcess' -count=1
```

Expected: pass, with processed raw intake output still containing existing `draft_artifact` plus new `proposal`.

- [ ] **Step 6: Commit**

```bash
git add internal/core/intake/proposal.go internal/core/intake/proposal_test.go internal/app/lifecycle/run.go
git commit -m "feat: add reviewable intake proposal envelope"
```

## Task 3: No-Promotion Default Proof

**Files:**
- Modify: `internal/app/lifecycle/run_test.go`
- Modify: `internal/app/lifecycle/run.go`

- [ ] **Step 1: Write failing negative-boundary test**

Add this test to `internal/app/lifecycle/run_test.go` near existing intake processing tests:

```go
func TestRunIntakeProcessCreatesProposalWithoutWorkByDefault(t *testing.T) {
	t.Parallel()

	root := testRepoRoot(t)
	var createOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{
		"intake", "raw", "create",
		"--text", "Draft an operator runbook for intake review proposals",
		"--json",
	}, strings.NewReader(""), &createOutput); err != nil {
		t.Fatalf("Run(intake raw create --text) error = %v", err)
	}

	var processOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"intake", "process", "--id", "intake-1", "--json"}, strings.NewReader(""), &processOutput); err != nil {
		t.Fatalf("Run(intake process) error = %v", err)
	}
	for _, want := range []string{
		`"status": "review_required"`,
		`"proposal"`,
		`"approval_posture": "needs_review"`,
		`"operator_next_action": "odin intake review show intake-1"`,
	} {
		if !strings.Contains(processOutput.String(), want) {
			t.Fatalf("process output = %s, want %s", processOutput.String(), want)
		}
	}

	var workStatus bytes.Buffer
	if err := Run(context.Background(), root, []string{"work", "status"}, strings.NewReader(""), &workStatus); err != nil {
		t.Fatalf("Run(work status) error = %v", err)
	}
	if output := workStatus.String(); !strings.Contains(output, "work_items=0") || !strings.Contains(output, "active_run_attempts=0") {
		t.Fatalf("work status output = %s, want no work or runs from process", output)
	}

	var logsOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"logs", "--json"}, strings.NewReader(""), &logsOutput); err != nil {
		t.Fatalf("Run(logs --json) error = %v", err)
	}
	for _, forbidden := range []string{`"type": "task.created"`, `"type": "run.created"`, `"type": "approval.created"`} {
		if strings.Contains(logsOutput.String(), forbidden) {
			t.Fatalf("logs output = %s, must not contain %s", logsOutput.String(), forbidden)
		}
	}
}
```

- [ ] **Step 2: Run failing test**

Run:

```bash
go test ./internal/app/lifecycle -run TestRunIntakeProcessCreatesProposalWithoutWorkByDefault -count=1
```

Expected: fail until `proposal` is serialized in routing notes and surfaced by `rawIntakeView`.

- [ ] **Step 3: Surface proposal in raw intake JSON**

In `rawIntakeItemView`, add:

```go
Proposal json.RawMessage `json:"proposal,omitempty"`
```

In `rawIntakeView`, after extracting `notes.Processing`, set:

```go
if notes.Proposal != nil {
	proposalJSON, err := json.Marshal(notes.Proposal)
	if err != nil {
		return rawIntakeItemView{}, err
	}
	view.Proposal = proposalJSON
}
```

Do not remove existing `processing` or `draft_artifact` fields; this task is additive.

- [ ] **Step 4: Run focused tests**

Run:

```bash
go test ./internal/app/lifecycle -run 'TestRunIntakeProcessCreatesProposalWithoutWorkByDefault|TestRunIntakeProcessCreatesReviewStatesWithoutExecution' -count=1
```

Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add internal/app/lifecycle/run.go internal/app/lifecycle/run_test.go
git commit -m "test: prove intake process stops before work"
```

## Task 4: Dedupe Canonical Link Hardening

**Files:**
- Modify: `internal/app/lifecycle/run.go`
- Modify: `internal/app/lifecycle/run_test.go`
- Modify: `internal/store/sqlite/intake_items_test.go`

- [ ] **Step 1: Write failing cooldown-aware duplicate test**

Add to `internal/app/lifecycle/run_test.go`:

```go
func TestRunIntakeProcessDoesNotLinkToArchivedCanonicalDuplicate(t *testing.T) {
	t.Parallel()

	root := testRepoRoot(t)
	createRaw := func() {
		t.Helper()
		if err := Run(context.Background(), root, []string{
			"intake", "raw", "create",
			"--text", "Prepare weekly intake summary for operator review",
			"--json",
		}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
			t.Fatalf("Run(intake raw create --text) error = %v", err)
		}
	}
	createRaw()
	if err := Run(context.Background(), root, []string{"intake", "process", "--id", "intake-1", "--json"}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatalf("Run(intake process intake-1) error = %v", err)
	}
	if err := Run(context.Background(), root, []string{"intake", "review", "archive", "intake-1", "--json"}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatalf("Run(intake review archive intake-1) error = %v", err)
	}

	createRaw()
	var processOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"intake", "process", "--id", "intake-2", "--json"}, strings.NewReader(""), &processOutput); err != nil {
		t.Fatalf("Run(intake process intake-2) error = %v", err)
	}
	if output := processOutput.String(); strings.Contains(output, `"canonical_intake_key": "intake-1"`) || strings.Contains(output, `"status": "duplicate_linked_or_suppressed"`) {
		t.Fatalf("process output = %s, want fresh proposal after archived canonical", output)
	}
	if output := processOutput.String(); !strings.Contains(output, `"status": "review_required"`) {
		t.Fatalf("process output = %s, want review_required", output)
	}
}
```

- [ ] **Step 2: Run failing test**

Run:

```bash
go test ./internal/app/lifecycle -run TestRunIntakeProcessDoesNotLinkToArchivedCanonicalDuplicate -count=1
```

Expected: fail if `findCanonicalDuplicate` links to archived/rejected/approval-denied canonical rows.

- [ ] **Step 3: Restrict canonical duplicate candidates to active states**

In `internal/app/lifecycle/run.go`, add:

```go
func isActiveCanonicalIntakeStatus(status string) bool {
	switch intake.CanonicalState(status) {
	case intake.StateReceived, intake.StateReviewRequired, intake.StateNeedsClarification:
		return true
	default:
		return false
	}
}
```

In `findCanonicalDuplicate`, skip candidates that are not active:

```go
if !isActiveCanonicalIntakeStatus(candidate.Status) {
	continue
}
```

This implements the product rule that stale or closed canonical items do not absorb new waves forever without introducing a new runtime duplicate aggregate.

- [ ] **Step 4: Add store-level duplicate link proof**

In `internal/store/sqlite/intake_items_test.go`, extend the existing duplicate test after `ProcessIntakeItem` or add a new test that creates two items, processes the second with `CanonicalIntakeItemID`, and asserts:

```go
if second.CanonicalIntakeItemID == nil || *second.CanonicalIntakeItemID != first.ID {
	t.Fatalf("CanonicalIntakeItemID = %+v, want %d", second.CanonicalIntakeItemID, first.ID)
}
if second.SuppressionReason == "" {
	t.Fatal("SuppressionReason is empty, want duplicate reason")
}
```

- [ ] **Step 5: Run tests**

Run:

```bash
go test ./internal/app/lifecycle ./internal/store/sqlite -run 'TestRunIntakeProcessDoesNotLinkToArchivedCanonicalDuplicate|TestCreateIntakeItem|Test.*Duplicate' -count=1
```

Expected: pass.

- [ ] **Step 6: Commit**

```bash
git add internal/app/lifecycle/run.go internal/app/lifecycle/run_test.go internal/store/sqlite/intake_items_test.go
git commit -m "feat: harden intake duplicate canonical links"
```

## Task 5: Overview And Status Alias Readback

**Files:**
- Modify: `internal/cli/overview/service.go`
- Modify: `internal/cli/overview/service_test.go`
- Modify: `internal/cli/render/overview_test.go` only if rendered output expectations need additive proposal language

- [ ] **Step 1: Write failing overview alias test**

In `internal/cli/overview/service_test.go`, add or extend a test that seeds raw intake statuses and asserts canonical counts:

```go
func TestOverviewIntakeInboxMapsCompatibilityStatuses(t *testing.T) {
	ctx := context.Background()
	env := newOverviewTestEnvironment(t)

	if _, err := env.store.CreateIntakeItem(ctx, sqlite.CreateIntakeItemParams{
		WorkspaceID:         env.workspaceID,
		SourceFamily:        "cli",
		EventKind:           "request",
		Subject:             "Review proposal",
		DedupeKey:           "overview-alias-1",
		DedupeRecipeVersion: "test-v1",
		SourceFactsJSON:     `{}`,
		Status:              "duplicate_linked_or_suppressed",
		Scope:               "project",
		ScopeKey:            "alpha",
		Summary:             "duplicate",
	}); err != nil {
		t.Fatalf("CreateIntakeItem(duplicate) error = %v", err)
	}
	if _, err := env.store.CreateIntakeItem(ctx, sqlite.CreateIntakeItemParams{
		WorkspaceID:         env.workspaceID,
		SourceFamily:        "cli",
		EventKind:           "request",
		Subject:             "Accepted proposal",
		DedupeKey:           "overview-alias-2",
		DedupeRecipeVersion: "test-v1",
		SourceFactsJSON:     `{}`,
		Status:              "accepted",
		Scope:               "project",
		ScopeKey:            "alpha",
		Summary:             "accepted",
	}); err != nil {
		t.Fatalf("CreateIntakeItem(accepted) error = %v", err)
	}

	view, err := Service{
		Store:            env.store,
		RegistrySnapshot: env.snapshot,
		Now:              time.Now,
	}.Build(ctx, scope.Resolution{Kind: scope.ScopeGlobal})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if view.IntakeInbox.DuplicateLinkedCount != 1 {
		t.Fatalf("DuplicateLinkedCount = %d, want 1", view.IntakeInbox.DuplicateLinkedCount)
	}
	if view.IntakeInbox.AcceptedCount != 1 {
		t.Fatalf("AcceptedCount = %d, want 1", view.IntakeInbox.AcceptedCount)
	}
}
```

- [ ] **Step 2: Run failing test**

Run:

```bash
go test ./internal/cli/overview -run TestOverviewIntakeInboxMapsCompatibilityStatuses -count=1
```

Expected: fail until overview maps through `intake.CanonicalState` or preserves both compatibility and canonical counts intentionally.

- [ ] **Step 3: Map states in overview**

In `internal/cli/overview/service.go`, import `internal/core/intake` and replace the raw status switch with canonical state mapping:

```go
status := intake.CanonicalState(item.Status)
if status != intake.StateReceived {
	view.IntakeInbox.RawProcessedCount++
}
if isReviewableIntakeStatus(item.Status) {
	view.IntakeInbox.ReviewQueueCount++
}
switch status {
case intake.StateReviewRequired:
	view.IntakeInbox.ReviewRequiredCount++
case intake.StateNeedsClarification:
	view.IntakeInbox.NeedsClarificationCount++
case intake.StateDuplicateLinked:
	view.IntakeInbox.DuplicateLinkedCount++
case intake.StateAcceptedForPromotion:
	view.IntakeInbox.AcceptedCount++
case intake.StateArchived:
	switch strings.ToLower(strings.TrimSpace(item.Status)) {
	case "rejected":
		view.IntakeInbox.RejectedCount++
	case "approval_denied":
		view.IntakeInbox.ApprovalDeniedCount++
	default:
		view.IntakeInbox.ArchivedCount++
	}
}
if strings.EqualFold(strings.TrimSpace(item.Status), "approval_required") {
	view.IntakeInbox.IntakeApprovalRequiredCount++
}
```

- [ ] **Step 4: Run overview tests**

Run:

```bash
go test ./internal/cli/overview ./internal/cli/render -run 'TestOverviewIntake|TestRenderOverview' -count=1
```

Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/overview/service.go internal/cli/overview/service_test.go internal/cli/render/overview_test.go
git commit -m "feat: align intake overview lifecycle aliases"
```

## Task 6: Real Odin E2E Proof

**Files:**
- Modify: `internal/app/lifecycle/run_test.go` if a test gap appears
- No new source files unless a failing proof exposes an implementation gap

- [ ] **Step 1: Build the repo-local binary**

Run:

```bash
go build -o ./bin/odin ./cmd/odin
```

Expected: exit 0 and produce `./bin/odin`.

- [ ] **Step 2: Verify operator binary path before live-style proof**

Run:

```bash
which odin
```

Expected: `/home/orchestrator/.local/bin/odin`.

Use `./bin/odin` for this repo-local post-build proof because the previous step built the exact branch under test.

- [ ] **Step 3: Run fresh-root intake E2E**

Run:

```bash
tmp_root="$(mktemp -d)"
ODIN_ROOT="$tmp_root" ./bin/odin intake raw create --text "Draft universal intake proposal proof" --json
ODIN_ROOT="$tmp_root" ./bin/odin intake raw show intake-1 --json
ODIN_ROOT="$tmp_root" ./bin/odin intake process --id intake-1 --json
ODIN_ROOT="$tmp_root" ./bin/odin intake review list --json
ODIN_ROOT="$tmp_root" ./bin/odin intake review show intake-1 --json
ODIN_ROOT="$tmp_root" ./bin/odin overview --json
ODIN_ROOT="$tmp_root" ./bin/odin work status
ODIN_ROOT="$tmp_root" ./bin/odin logs --json
rm -rf "$tmp_root"
```

Expected:

- raw create returns `status=received` or JSON equivalent
- process returns `review_required` and includes `proposal`
- review show includes the Reviewable Intake Proposal envelope
- overview reports one raw intake item and one review queue item
- work status reports `work_items=0` and `active_run_attempts=0`
- logs contain intake events and no `task.created` or run creation event

- [ ] **Step 4: Run focused and full verification**

Run:

```bash
go test ./internal/core/intake ./internal/app/lifecycle ./internal/store/sqlite ./internal/cli/overview ./internal/cli/render -count=1
go test ./...
```

Expected: both commands pass.

- [ ] **Step 5: Commit any proof-driven fixes**

If Step 3 or Step 4 required fixes, commit only those touched files:

```bash
git status --short
git add internal/core/intake internal/app/lifecycle/run.go internal/app/lifecycle/run_test.go internal/store/sqlite/intake_items_test.go internal/cli/overview/service.go internal/cli/overview/service_test.go internal/cli/render/overview_test.go docs/contracts/external-intake.md docs/contracts/runtime-events.md
git commit -m "fix: complete universal intake proof"
```

If no fixes were required, do not create an empty commit.

## Task 7: Contract Documentation Alignment

**Files:**
- Modify: `docs/contracts/external-intake.md`
- Modify: `docs/contracts/runtime-events.md`
- Modify: `docs/superpowers/specs/2026-05-10-universal-intake-system-design.md` only if implementation changed an approved term

- [ ] **Step 1: Update `docs/contracts/external-intake.md`**

Add a "Universal Source Envelope" section that documents:

```markdown
## Universal Source Envelope

External sources normalize into `source_family`, `external_object_id`, `event_kind`, `observed_at`, `subject`, `body` or `summary`, `actor`, `source_uri`, `evidence_refs`, and namespaced `adapter_facts`.

Adapters normalize source facts. Odin core owns `dedupe_key`, `dedupe_recipe_version`, lifecycle state, and promotion boundaries.

Raw intake processing may create a Reviewable Intake Proposal. It must not create executable Work Items, Run Attempts, dispatches, or external mutations by default.
```

- [ ] **Step 2: Update `docs/contracts/runtime-events.md`**

Add an "Intake-to-proposal expectation" section:

```markdown
## Intake-to-proposal expectation

Raw intake processing remains on the `intake_items` SQLite authority. Processing may emit `intake.processing_started`, `intake.classified`, `intake.dedupe_reviewed`, `intake.routed`, `intake.draft_artifact_created`, `intake.clarification_needed`, `intake.duplicate_linked_or_suppressed`, and `intake.processed`.

The processing payload and routing notes must preserve enough evidence to reconstruct the Reviewable Intake Proposal. Intake processing must not create Work Items, Run Attempts, dispatches, approvals, or external mutations by default.
```

- [ ] **Step 3: Run docs and targeted tests**

Run:

```bash
rg -n "Reviewable Intake Proposal|Universal Source Envelope|Intake-to-proposal expectation" docs/contracts docs/superpowers/specs/2026-05-10-universal-intake-system-design.md
go test ./internal/core/intake ./internal/app/lifecycle -count=1
```

Expected: `rg` finds the new contract sections and tests pass.

- [ ] **Step 4: Commit**

```bash
git add docs/contracts/external-intake.md docs/contracts/runtime-events.md docs/superpowers/specs/2026-05-10-universal-intake-system-design.md
git commit -m "docs: align universal intake contracts"
```

## Final Verification

- [ ] **Step 1: Confirm no accidental workspace mess**

Run:

```bash
git status --short --untracked-files=all
```

Expected: only intentional tracked changes for the implementation branch, plus any pre-existing unrelated dirty files that the worker did not touch. No scratch files, generated temporary folders, or dead files.

- [ ] **Step 2: Verify operator path**

Run:

```bash
which odin
```

Expected: `/home/orchestrator/.local/bin/odin`.

- [ ] **Step 3: Run final test suite**

Run:

```bash
go test ./...
```

Expected: pass.

- [ ] **Step 4: Run final real Odin proof**

Run the fresh-root proof from Task 6 again with the repo-built `./bin/odin`.

Expected: raw intake becomes a Reviewable Intake Proposal; no Work Item, Run Attempt, dispatch, or external mutation is created before explicit promotion.

- [ ] **Step 5: Summarize evidence**

Final implementation report must include:

- Existing state found
- Reused components
- New components added
- Why new components were necessary
- Real `odin` command E2E checks performed
- Remaining risks, especially whether `review accept` remains the explicit promotion boundary or a non-promoting review action was added
