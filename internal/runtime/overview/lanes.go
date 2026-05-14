package overview

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"odin-os/internal/registry"
	"odin-os/internal/runtime/reviewqueue"
	"odin-os/internal/store/sqlite"
	toolcatalog "odin-os/internal/tools/catalog"
)

type ReadinessInput struct {
	Status       string
	HealthStatus string
}

func BuildReadiness(input ReadinessInput) ReadinessLane {
	status := strings.ToLower(strings.TrimSpace(input.Status))
	health := strings.ToLower(strings.TrimSpace(input.HealthStatus))
	lane := ReadinessLane{
		Wiring:       WiringLive,
		Status:       status,
		HealthStatus: health,
	}
	if lane.Status == "" {
		lane.Status = "unknown"
	}
	if lane.HealthStatus == "" {
		lane.HealthStatus = "unknown"
	}
	lane.Ready = lane.Status == "ready" && lane.HealthStatus == "healthy"
	if lane.Status == "unknown" || lane.HealthStatus == "unknown" {
		lane.Note = "readiness or health was not provided by the caller; unknown is not treated as healthy"
	}
	return lane
}

type BinarySourceInput struct {
	BinaryPath string
	SourceRoot string
}

func BuildBinarySource(input BinarySourceInput) BinarySourceLane {
	binaryPath := strings.TrimSpace(input.BinaryPath)
	sourceRoot := strings.TrimSpace(input.SourceRoot)
	lane := BinarySourceLane{
		Wiring:     WiringLive,
		Status:     "unknown",
		BinaryPath: binaryPath,
		SourceRoot: sourceRoot,
	}
	if binaryPath == "" || sourceRoot == "" {
		lane.Note = "binary path or source root was not provided by the caller"
		return lane
	}
	rel, err := filepath.Rel(filepath.Clean(sourceRoot), filepath.Clean(binaryPath))
	if err == nil && rel != "." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".." {
		lane.Status = "aligned"
		return lane
	}
	lane.Status = "external_binary"
	return lane
}

func BuildDeliveryProfiles(snapshot registry.Snapshot) DeliveryProfileLane {
	lane := DeliveryProfileLane{Wiring: WiringCatalogBacked}
	for _, item := range snapshot.Items {
		if item.Kind != registry.KindWorkflow || !hasRegistryTag(item, "delivery_profile") {
			continue
		}
		lane.ProfileCount++
		lane.Keys = append(lane.Keys, item.Key)
	}
	sort.Strings(lane.Keys)
	return lane
}

type ExecutionIntentInput struct {
	ExecutionIntent string
}

func BuildExecutionIntent(items []ExecutionIntentInput) ExecutionIntentLane {
	lane := ExecutionIntentLane{Wiring: WiringLive}
	for _, item := range items {
		if strings.TrimSpace(item.ExecutionIntent) == "" {
			lane.FallbackWorkItemCount++
			continue
		}
		lane.ExplicitWorkItemCount++
	}
	return lane
}

type ActualUseInput struct {
	WorkItemCount                 int
	OpenWorkItemCount             int
	ActiveRunCount                int
	PendingApprovalCount          int
	ReviewQueueCount              int
	BlockedWorkItemCount          int
	FailedWorkItemCount           int
	RecoveryRecommendationCount   int
	IntakeReviewCount             int
	AutomationTriggerCount        int
	EnabledAutomationTriggerCount int
	FollowUpObligationCount       int
	DueFollowUpObligationCount    int
	DeliveryProfileCount          int
	ExplicitIntentWorkItemCount   int
	FallbackIntentWorkItemCount   int
}

func BuildActualUse(input ActualUseInput) ActualUseLane {
	lane := ActualUseLane{
		Wiring:                        WiringLive,
		WorkItemCount:                 input.WorkItemCount,
		OpenWorkItemCount:             input.OpenWorkItemCount,
		ActiveRunCount:                input.ActiveRunCount,
		PendingApprovalCount:          input.PendingApprovalCount,
		ReviewQueueCount:              input.ReviewQueueCount,
		BlockedWorkItemCount:          input.BlockedWorkItemCount,
		FailedWorkItemCount:           input.FailedWorkItemCount,
		RecoveryRecommendationCount:   input.RecoveryRecommendationCount,
		IntakeReviewCount:             input.IntakeReviewCount,
		AutomationTriggerCount:        input.AutomationTriggerCount,
		EnabledAutomationTriggerCount: input.EnabledAutomationTriggerCount,
		FollowUpObligationCount:       input.FollowUpObligationCount,
		DueFollowUpObligationCount:    input.DueFollowUpObligationCount,
		DeliveryProfileCount:          input.DeliveryProfileCount,
		ExplicitIntentWorkItemCount:   input.ExplicitIntentWorkItemCount,
		FallbackIntentWorkItemCount:   input.FallbackIntentWorkItemCount,
	}
	lane.ActionRequiredCount = lane.ReviewQueueCount + lane.BlockedWorkItemCount
	if lane.ActionRequiredCount > 0 {
		lane.Status = "action_required"
	} else {
		lane.Status = "clear"
	}
	return lane
}

func BuildReviewQueue(projection reviewqueue.Projection) ReviewQueueLane {
	return ReviewQueueLane{
		Wiring:              WiringLive,
		TotalCount:          projection.TotalCount,
		IntakeCount:         projection.IntakeCount,
		GoalCount:           projection.GoalCount,
		ApprovalCount:       projection.ApprovalCount,
		KnowledgeCount:      projection.KnowledgeCount,
		SkillArtifactCount:  projection.SkillArtifactCount,
		MemoryProposalCount: projection.MemoryProposalCount,
		RecoveryCount:       projection.RecoveryCount,
		FailedWorkCount:     projection.FailedWorkCount,
	}
}

func BuildCapabilityTruth(catalog CapabilityCatalogLane, snapshot registry.Snapshot, tools map[string]toolcatalog.ToolDefinition) CapabilityTruthLane {
	lane := CapabilityTruthLane{
		Wiring:            WiringLive,
		AuthoredInventory: catalog,
		AuthoredAssetCount: catalog.AgentDefinitionCount +
			catalog.SkillCount +
			catalog.WorkflowCount +
			catalog.CommandCount +
			catalog.ToolCount,
		Notes: []string{
			"Registry prompts are authored assets until runtime invocation, persistence/output, policy, and audit evidence exist.",
		},
	}

	for _, item := range snapshot.Items {
		lane.appendTruthSummary(capabilityTruthFromRegistryItem(item))
	}

	toolKeys := make([]string, 0, len(tools))
	for key := range tools {
		toolKeys = append(toolKeys, key)
	}
	sort.Strings(toolKeys)
	for _, key := range toolKeys {
		lane.appendTruthSummary(capabilityTruthFromBuiltinTool(tools[key]))
	}

	sort.SliceStable(lane.Items, func(i int, j int) bool {
		if lane.Items[i].Kind != lane.Items[j].Kind {
			return lane.Items[i].Kind < lane.Items[j].Kind
		}
		return lane.Items[i].Key < lane.Items[j].Key
	})

	return lane
}

func (lane *CapabilityTruthLane) appendTruthSummary(summary CapabilityTruthSummary) {
	if summary.Key == "" {
		return
	}

	lane.Items = append(lane.Items, summary)
	if summary.HighRisk {
		lane.HighRiskFamilyCount++
	}

	switch summary.TruthLevel {
	case "runtime_proven":
		lane.RuntimeProvenCount++
	case "partial":
		lane.PartialCount++
	case "authored_asset", "approval_required", "read_only", "unsupported":
		lane.AdvisoryCount++
	default:
		lane.UnknownCount++
	}
}

func capabilityTruthFromRegistryItem(item registry.Item) CapabilityTruthSummary {
	summary := CapabilityTruthSummary{
		Kind:       string(item.Kind),
		Key:        item.Key,
		Title:      item.Title,
		TruthLevel: "authored_asset",
		Proof:      []string{registrySourceProof(item)},
	}

	switch item.Kind {
	case registry.KindAgent:
		if item.Delegation.Enabled && normalizeOperatorSurface(item.Delegation.OperatorSurface) == "companion_delegate" {
			summary.TruthLevel = "runtime_proven"
			summary.CountsAsImplemented = true
			summary.Proof = []string{"odin companion delegate", "jobs", "runs", "delegations", "logs"}
		}
	case registry.KindSkill:
		summary.TruthLevel = "partial"
		summary.Proof = append(summary.Proof, "skill registry")
	case registry.KindWorkflow:
		summary.Proof = append(summary.Proof, "workflow registry")
	case registry.KindCommand:
		summary.Proof = append(summary.Proof, "command registry")
	case registry.KindTool:
		summary.TruthLevel = "partial"
		summary.Proof = append(summary.Proof, "tool registry")
	}

	if highRisk, label := capabilityRiskLabel(summary.Key, item.Title, item.Tags, item.Permissions); highRisk {
		summary.HighRisk = true
		summary.RiskLabel = label
	}

	return summary
}

func capabilityTruthFromBuiltinTool(definition toolcatalog.ToolDefinition) CapabilityTruthSummary {
	summary := CapabilityTruthSummary{
		Kind:       string(registry.KindTool),
		Key:        definition.Key,
		Title:      definition.Title,
		TruthLevel: "partial",
		Proof:      []string{"builtin tool catalog", "capability gateway"},
	}
	if definition.Invoke != nil {
		summary.Proof = append(summary.Proof, "invoke function registered")
	}

	if highRisk, label := capabilityRiskLabel(definition.Key, definition.Title, definition.Tags, definition.Scopes); highRisk {
		summary.HighRisk = true
		summary.RiskLabel = label
		if label == "approval_required" && !definition.RequiresApproval {
			summary.TruthLevel = "approval_required"
			summary.Proof = append(summary.Proof, "approval required")
		}
	}
	if definition.RequiresApproval {
		summary.HighRisk = true
		summary.RiskLabel = "approval_required"
		summary.TruthLevel = "approval_required"
		summary.Proof = append(summary.Proof, "approval required")
	}

	return summary
}

func registrySourceProof(item registry.Item) string {
	if item.Source.RelativePath != "" {
		return item.Source.RelativePath
	}
	if item.Kind != "" {
		return "registry/" + string(item.Kind)
	}
	return "registry"
}

func normalizeOperatorSurface(value string) string {
	normalized := strings.TrimSpace(strings.ToLower(value))
	normalized = strings.ReplaceAll(normalized, " ", "_")
	normalized = strings.ReplaceAll(normalized, "-", "_")
	return normalized
}

func capabilityRiskLabel(key, title string, tags, permissions []string) (bool, string) {
	haystack := []string{key, title}
	haystack = append(haystack, tags...)
	haystack = append(haystack, permissions...)
	joined := strings.ToLower(strings.Join(haystack, " "))

	switch {
	case strings.Contains(joined, "visible_evidence"), strings.Contains(joined, "evidence bundle"), strings.Contains(joined, "evidence_bundle"):
		return true, "read_only"
	case strings.Contains(joined, "publish"), strings.Contains(joined, "post"), strings.Contains(joined, "send"):
		return true, "approval_required"
	case strings.Contains(joined, "delete"), strings.Contains(joined, "deploy"), strings.Contains(joined, "production"), strings.Contains(joined, "permission"):
		return true, "approval_required"
	case strings.Contains(joined, "calendar"), strings.Contains(joined, "email"), strings.Contains(joined, "finance"), strings.Contains(joined, "github"), strings.Contains(joined, "browser"):
		return true, "read_only"
	default:
		return false, ""
	}
}

func hasRegistryTag(item registry.Item, tag string) bool {
	for _, candidate := range item.Tags {
		if strings.EqualFold(strings.TrimSpace(candidate), tag) {
			return true
		}
	}
	return false
}

func ApplyBrowserEvidenceDetails(item *BrowserEvidenceSummary, details string) {
	if item == nil || strings.TrimSpace(details) == "" {
		return
	}
	var payload struct {
		PageTitle     string                `json:"page_title"`
		URL           string                `json:"url"`
		SelectedLinks []BrowserEvidenceLink `json:"selected_links"`
		Confidence    string                `json:"confidence"`
		Limitations   []string              `json:"limitations"`
	}
	if err := json.Unmarshal([]byte(details), &payload); err != nil {
		return
	}
	item.PageTitle = payload.PageTitle
	item.URL = payload.URL
	item.SelectedLinks = payload.SelectedLinks
	item.Confidence = payload.Confidence
	item.Limitations = payload.Limitations
}

func IsReviewableIntakeStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "review_required", "needs_clarification", "duplicate_linked_or_suppressed", "approval_required":
		return true
	default:
		return false
	}
}

func RawIntakeSummaryFromItem(item sqlite.IntakeItem) RawIntakeSummary {
	return RawIntakeSummary{
		ID:          item.ID,
		Key:         fmt.Sprintf("intake-%d", item.ID),
		ProjectKey:  rawIntakeProjectKey(item),
		Source:      item.SourceFamily,
		IntakeType:  item.EventKind,
		DedupKey:    item.DedupeKey,
		RequestedBy: rawIntakeRequestedBy(item.SourceFactsJSON),
		Title:       item.Subject,
		Status:      item.Status,
		Summary:     item.Summary,
		CreatedAt:   item.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:   item.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func rawIntakeProjectKey(item sqlite.IntakeItem) string {
	switch strings.TrimSpace(item.Scope) {
	case "project", "odin-core":
		return strings.TrimSpace(item.ScopeKey)
	default:
		return ""
	}
}

func rawIntakeRequestedBy(sourceFactsJSON string) string {
	var facts struct {
		RequestedBy string `json:"requested_by"`
	}
	if err := json.Unmarshal([]byte(sourceFactsJSON), &facts); err != nil {
		return ""
	}
	return facts.RequestedBy
}

type IntakeStatusInput struct {
	IntakeApprovalRequiredCount int
	ReviewQueueCount            int
	RawProcessedCount           int
}

func IntakeLaneStatus(input IntakeStatusInput) string {
	switch {
	case input.IntakeApprovalRequiredCount > 0:
		return "approval_pending"
	case input.ReviewQueueCount > 0:
		return "review_pending"
	case input.RawProcessedCount > 0:
		return "processed"
	default:
		return "received"
	}
}
