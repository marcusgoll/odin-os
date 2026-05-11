package memoryproposal

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"odin-os/internal/store/sqlite"
)

const (
	SchemaV1        = "memory_proposal.v1"
	StatusPending   = "pending"
	StatusAccepted  = "accepted"
	StatusRejected  = "rejected"
	StatusArchived  = "archived"
	ApprovalPending = "pending"
)

type Service struct {
	Store *sqlite.Store
}

type Source struct {
	Type            string `json:"type"`
	ID              string `json:"id,omitempty"`
	Key             string `json:"key,omitempty"`
	URL             string `json:"url,omitempty"`
	ContextPacketID int64  `json:"context_packet_id,omitempty"`
}

type Provenance struct {
	CreatedBy    string `json:"created_by"`
	CreatedVia   string `json:"created_via"`
	ReviewedBy   string `json:"reviewed_by,omitempty"`
	ReviewReason string `json:"review_reason,omitempty"`
}

type Safety struct {
	Sensitivity      string `json:"sensitivity"`
	Redacted         bool   `json:"redacted"`
	RestrictedRecall bool   `json:"restricted_recall"`
}

type Details struct {
	Schema     string     `json:"schema"`
	Status     string     `json:"status"`
	Approval   string     `json:"approval"`
	Source     Source     `json:"source"`
	Provenance Provenance `json:"provenance"`
	Safety     Safety     `json:"safety"`
}

type Proposal struct {
	ID             int64     `json:"id"`
	QueueID        string    `json:"queue_id"`
	Scope          string    `json:"scope"`
	ScopeKey       string    `json:"scope_key"`
	ProjectKey     string    `json:"project_key,omitempty"`
	MemoryType     string    `json:"memory_type"`
	Summary        string    `json:"summary"`
	Status         string    `json:"status"`
	Approval       string    `json:"approval"`
	Active         bool      `json:"active"`
	SourceType     string    `json:"source_type,omitempty"`
	SourceID       string    `json:"source_id,omitempty"`
	SourceKey      string    `json:"source_key,omitempty"`
	Sensitivity    string    `json:"sensitivity,omitempty"`
	AllowedActions []string  `json:"allowed_actions"`
	Details        Details   `json:"details"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type ProposeParams struct {
	Scope       string
	ProjectKey  string
	MemoryType  string
	Summary     string
	SourceType  string
	SourceID    string
	SourceKey   string
	SourceURL   string
	Sensitivity string
	Redacted    bool
	CreatedBy   string
}

type ListParams struct {
	Scope      string
	ProjectKey string
	MemoryType string
	Status     string
}

type ResolveParams struct {
	ID         int64
	Decision   string
	ReviewedBy string
	Reason     string
}

func (service Service) Propose(ctx context.Context, params ProposeParams) (Proposal, error) {
	if service.Store == nil {
		return Proposal{}, fmt.Errorf("memory store is required")
	}
	params.Scope = strings.ToLower(strings.TrimSpace(params.Scope))
	params.ProjectKey = strings.TrimSpace(params.ProjectKey)
	params.MemoryType = strings.TrimSpace(params.MemoryType)
	params.Summary = strings.TrimSpace(params.Summary)
	params.SourceType = strings.TrimSpace(params.SourceType)
	params.SourceID = strings.TrimSpace(params.SourceID)
	params.SourceKey = strings.TrimSpace(params.SourceKey)
	params.SourceURL = strings.TrimSpace(params.SourceURL)
	params.Sensitivity = strings.ToLower(strings.TrimSpace(params.Sensitivity))
	if params.CreatedBy == "" {
		params.CreatedBy = "operator"
	}
	if params.Scope == "" {
		return Proposal{}, fmt.Errorf("memory proposal scope is required")
	}
	if params.MemoryType == "" {
		return Proposal{}, fmt.Errorf("memory proposal type is required")
	}
	if params.Summary == "" {
		return Proposal{}, fmt.Errorf("memory proposal summary is required")
	}
	if params.SourceType == "" {
		return Proposal{}, fmt.Errorf("memory proposal source_type is required")
	}
	if params.SourceID == "" && params.SourceKey == "" && params.SourceURL == "" {
		return Proposal{}, fmt.Errorf("memory proposal requires source_id, source_key, or source_url")
	}
	if params.Sensitivity == "" {
		return Proposal{}, fmt.Errorf("memory proposal sensitivity is required")
	}
	scopeKey := params.ProjectKey
	var projectID *int64
	if params.Scope == "project" {
		if params.ProjectKey == "" {
			return Proposal{}, fmt.Errorf("project memory proposals require project")
		}
		project, err := service.Store.GetProjectByKey(ctx, params.ProjectKey)
		if err != nil {
			return Proposal{}, err
		}
		projectID = &project.ID
		scopeKey = project.Key
	}
	if params.Scope == "global" {
		scopeKey = "global"
	}
	if scopeKey == "" {
		return Proposal{}, fmt.Errorf("memory proposal scope_key is required")
	}
	details := Details{
		Schema:   SchemaV1,
		Status:   StatusPending,
		Approval: ApprovalPending,
		Source: Source{
			Type: params.SourceType,
			ID:   params.SourceID,
			Key:  params.SourceKey,
			URL:  params.SourceURL,
		},
		Provenance: Provenance{
			CreatedBy:  params.CreatedBy,
			CreatedVia: "memory.propose",
		},
		Safety: Safety{
			Sensitivity: params.Sensitivity,
			Redacted:    params.Redacted,
		},
	}
	detailsJSON, err := EncodeDetails(details)
	if err != nil {
		return Proposal{}, err
	}
	summary, err := service.Store.RecordMemorySummary(ctx, sqlite.RecordMemorySummaryParams{
		ProjectID:   projectID,
		Scope:       params.Scope,
		ScopeKey:    scopeKey,
		MemoryType:  params.MemoryType,
		Summary:     params.Summary,
		DetailsJSON: detailsJSON,
	})
	if err != nil {
		return Proposal{}, err
	}
	return service.proposalFromSummary(ctx, summary)
}

func (service Service) List(ctx context.Context, params ListParams) ([]Proposal, error) {
	if service.Store == nil {
		return nil, fmt.Errorf("memory store is required")
	}
	projectID, projectKey, err := service.resolveProject(ctx, params.ProjectKey)
	if err != nil {
		return nil, err
	}
	summaries, err := service.Store.ListMemorySummaries(ctx, sqlite.ListMemorySummariesParams{
		ProjectID:  projectID,
		Scope:      strings.TrimSpace(params.Scope),
		MemoryType: strings.TrimSpace(params.MemoryType),
	})
	if err != nil {
		return nil, err
	}
	status := strings.ToLower(strings.TrimSpace(params.Status))
	proposals := make([]Proposal, 0, len(summaries))
	for _, summary := range summaries {
		proposal, err := service.proposalFromSummary(ctx, summary)
		if err != nil {
			return nil, err
		}
		if projectKey != "" && proposal.ProjectKey == "" {
			proposal.ProjectKey = projectKey
		}
		if includeProposalForStatus(proposal, status) {
			proposals = append(proposals, proposal)
		}
	}
	return proposals, nil
}

func (service Service) Get(ctx context.Context, id int64) (Proposal, error) {
	if service.Store == nil {
		return Proposal{}, fmt.Errorf("memory store is required")
	}
	if id <= 0 {
		return Proposal{}, fmt.Errorf("memory proposal id must be positive")
	}
	summaries, err := service.Store.ListMemorySummaries(ctx, sqlite.ListMemorySummariesParams{})
	if err != nil {
		return Proposal{}, err
	}
	for _, summary := range summaries {
		if summary.ID == id {
			return service.proposalFromSummary(ctx, summary)
		}
	}
	return Proposal{}, sql.ErrNoRows
}

func (service Service) Resolve(ctx context.Context, params ResolveParams) (Proposal, bool, error) {
	if service.Store == nil {
		return Proposal{}, false, fmt.Errorf("memory store is required")
	}
	decision := strings.ToLower(strings.TrimSpace(params.Decision))
	status, err := statusForDecision(decision)
	if err != nil {
		return Proposal{}, false, err
	}
	if strings.TrimSpace(params.ReviewedBy) == "" {
		params.ReviewedBy = "operator"
	}
	if strings.TrimSpace(params.Reason) == "" {
		params.Reason = "unified review decision"
	}
	current, err := service.Get(ctx, params.ID)
	if err != nil {
		return Proposal{}, false, err
	}
	if current.Details.Schema != SchemaV1 {
		return Proposal{}, false, fmt.Errorf("memory proposal %d is not a v1 memory proposal", params.ID)
	}
	repeated := current.Status == status
	details := current.Details
	details.Status = status
	details.Approval = status
	details.Provenance.ReviewedBy = strings.TrimSpace(params.ReviewedBy)
	details.Provenance.ReviewReason = strings.TrimSpace(params.Reason)
	encoded, err := EncodeDetails(details)
	if err != nil {
		return Proposal{}, false, err
	}
	updated, err := service.Store.UpdateMemorySummaryDetails(ctx, sqlite.UpdateMemorySummaryDetailsParams{
		MemoryID:    params.ID,
		DetailsJSON: encoded,
	})
	if err != nil {
		return Proposal{}, false, err
	}
	proposal, err := service.proposalFromSummary(ctx, updated)
	return proposal, repeated, err
}

func ParseRef(ref string) (int64, error) {
	ref = strings.TrimSpace(ref)
	ref = strings.TrimPrefix(ref, "memory-proposal:")
	id, err := strconv.ParseInt(ref, 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("memory proposal id must be a positive integer")
	}
	return id, nil
}

func EncodeDetails(details Details) (string, error) {
	encoded, err := json.Marshal(details)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func DecodeDetails(raw string) (Details, bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Details{}, false, nil
	}
	var details Details
	if err := json.Unmarshal([]byte(raw), &details); err != nil {
		return Details{}, false, err
	}
	if details.Schema != SchemaV1 {
		return Details{}, false, nil
	}
	if details.Status == "" {
		details.Status = StatusPending
	}
	if details.Approval == "" {
		details.Approval = details.Status
	}
	return details, true, nil
}

func (service Service) proposalFromSummary(ctx context.Context, summary sqlite.MemorySummary) (Proposal, error) {
	details, ok, err := DecodeDetails(summary.DetailsJSON)
	if err != nil {
		return Proposal{}, err
	}
	if !ok {
		details = legacyDetails(summary.DetailsJSON)
	}
	projectKey := ""
	if summary.ProjectID != nil {
		if project, err := service.Store.GetProject(ctx, *summary.ProjectID); err == nil {
			projectKey = project.Key
		}
	}
	status := details.Status
	if status == "" {
		status = StatusAccepted
	}
	approval := details.Approval
	if approval == "" {
		approval = status
	}
	return Proposal{
		ID:             summary.ID,
		QueueID:        fmt.Sprintf("memory-proposal:%d", summary.ID),
		Scope:          summary.Scope,
		ScopeKey:       summary.ScopeKey,
		ProjectKey:     projectKey,
		MemoryType:     summary.MemoryType,
		Summary:        summary.Summary,
		Status:         status,
		Approval:       approval,
		Active:         IsActiveStatus(status),
		SourceType:     details.Source.Type,
		SourceID:       details.Source.ID,
		SourceKey:      details.Source.Key,
		Sensitivity:    details.Safety.Sensitivity,
		AllowedActions: AllowedActions(status),
		Details:        details,
		CreatedAt:      summary.CreatedAt,
		UpdatedAt:      summary.UpdatedAt,
	}, nil
}

func legacyDetails(raw string) Details {
	fields := map[string]string{}
	var payload struct {
		Fields map[string]string `json:"fields"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err == nil {
		fields = payload.Fields
	}
	status := strings.TrimSpace(fields["approval"])
	if status == "" {
		status = strings.TrimSpace(fields["status"])
	}
	return Details{
		Status:   status,
		Approval: status,
		Source: Source{
			Type: fields["source_type"],
			ID:   fields["source_id"],
			Key:  fields["source_key"],
		},
		Safety: Safety{
			Sensitivity: fields["sensitivity"],
		},
	}
}

func includeProposalForStatus(proposal Proposal, status string) bool {
	switch status {
	case "":
		return proposal.Active
	case "all":
		return true
	default:
		return strings.EqualFold(proposal.Status, status)
	}
}

func (service Service) resolveProject(ctx context.Context, projectKey string) (*int64, string, error) {
	projectKey = strings.TrimSpace(projectKey)
	if projectKey == "" {
		return nil, "", nil
	}
	project, err := service.Store.GetProjectByKey(ctx, projectKey)
	if err != nil {
		return nil, "", err
	}
	return &project.ID, project.Key, nil
}

func statusForDecision(decision string) (string, error) {
	switch decision {
	case "accept", "approve", "accepted":
		return StatusAccepted, nil
	case "reject", "rejected":
		return StatusRejected, nil
	case "archive", "archived":
		return StatusArchived, nil
	default:
		return "", fmt.Errorf("memory proposal action must be accept, reject, or archive")
	}
}

func IsActiveStatus(status string) bool {
	return strings.EqualFold(strings.TrimSpace(status), StatusAccepted)
}

func AllowedActions(status string) []string {
	if strings.EqualFold(strings.TrimSpace(status), StatusPending) {
		return []string{"accept", "reject", "archive"}
	}
	return []string{}
}
