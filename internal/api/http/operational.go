package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	healthsvc "odin-os/internal/runtime/health"
	"odin-os/internal/runtime/projections"
	"odin-os/internal/store/sqlite"
	metricsvc "odin-os/internal/telemetry/metrics"
)

type Dependencies struct {
	Store           *sqlite.Store
	Health          healthsvc.Service
	Metrics         metricsvc.Service
	RegistryHealthy bool
}

func NewOperationalHandler(deps Dependencies) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(writer http.ResponseWriter, request *http.Request) {
		report, err := deps.Health.Doctor(request.Context(), deps.RegistryHealthy)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusServiceUnavailable)
			return
		}
		writeJSON(writer, http.StatusOK, report)
	})
	mux.HandleFunc("/readyz", func(writer http.ResponseWriter, request *http.Request) {
		report, err := deps.Health.Doctor(request.Context(), deps.RegistryHealthy)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusServiceUnavailable)
			return
		}

		statusCode := http.StatusOK
		if report.Status != healthsvc.StatusHealthy {
			statusCode = http.StatusServiceUnavailable
		}
		writeJSON(writer, statusCode, report)
	})
	mux.HandleFunc("/metrics", func(writer http.ResponseWriter, request *http.Request) {
		snapshot, err := deps.Metrics.Collect(request.Context())
		if err != nil {
			http.Error(writer, err.Error(), http.StatusServiceUnavailable)
			return
		}

		writer.Header().Set("Content-Type", "text/plain; version=0.0.4")
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte(metricsvc.Render(snapshot)))
	})
	mux.HandleFunc("/workspace", func(writer http.ResponseWriter, request *http.Request) {
		if deps.Store == nil {
			http.Error(writer, "workspace store unavailable", http.StatusServiceUnavailable)
			return
		}

		payload, err := loadWorkspacePayload(request.Context(), deps.Store, request.URL.Query().Get("key"))
		if err != nil {
			statusCode := http.StatusInternalServerError
			if errors.Is(err, errWorkspaceNotFound) {
				statusCode = http.StatusNotFound
			}
			http.Error(writer, err.Error(), statusCode)
			return
		}
		writeJSON(writer, http.StatusOK, payload)
	})
	return mux
}

func writeJSON(writer http.ResponseWriter, statusCode int, payload any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(statusCode)
	_ = json.NewEncoder(writer).Encode(payload)
}

var errWorkspaceNotFound = errors.New("workspace not found")

type workspacePayload struct {
	Workspace           workspaceSummaryPayload     `json:"workspace"`
	Initiatives         []initiativePayload         `json:"initiatives"`
	InitiativeWorkItems []initiativeWorkItemPayload `json:"initiative_work_items"`
	BlockedItems        []blockedItemPayload        `json:"blocked_items"`
	PendingApprovals    []pendingApprovalPayload    `json:"pending_approvals"`
}

type workspaceSummaryPayload struct {
	Key                  string `json:"key"`
	Name                 string `json:"name"`
	InitiativeCount      int    `json:"initiative_count"`
	CompanionCount       int    `json:"companion_count"`
	PendingApprovalCount int    `json:"pending_approval_count"`
	BlockedItemCount     int    `json:"blocked_item_count"`
}

type initiativePayload struct {
	Key               string `json:"key"`
	Title             string `json:"title"`
	Kind              string `json:"kind"`
	Status            string `json:"status"`
	ProjectKey        string `json:"project_key"`
	OwnerCompanionKey string `json:"owner_companion_key"`
	OpenWorkItemCount int    `json:"open_work_item_count"`
}

type blockedItemPayload struct {
	TaskKey       string `json:"task_key"`
	ProjectKey    string `json:"project_key"`
	InitiativeKey string `json:"initiative_key"`
	CompanionKey  string `json:"companion_key"`
	Reason        string `json:"reason"`
	NextStep      string `json:"next_step"`
}

type pendingApprovalPayload struct {
	ProjectKey string `json:"project_key"`
	TaskKey    string `json:"task_key"`
	Status     string `json:"status"`
}

type initiativeWorkItemPayload struct {
	InitiativeKey string `json:"initiative_key"`
	TaskKey       string `json:"task_key"`
	ProjectKey    string `json:"project_key"`
	Title         string `json:"title"`
	Status        string `json:"status"`
}

func loadWorkspacePayload(ctx context.Context, store *sqlite.Store, requestedWorkspaceKey string) (workspacePayload, error) {
	home, err := findWorkspaceHome(ctx, store, requestedWorkspaceKey)
	if err != nil {
		return workspacePayload{}, err
	}

	initiatives, err := projections.ListInitiativePortfolioViews(ctx, store.DB(), home.WorkspaceKey)
	if err != nil {
		return workspacePayload{}, err
	}
	initiativeWorkItems := make([]projections.InitiativeWorkItemView, 0, len(initiatives))
	for _, initiative := range initiatives {
		workItems, err := projections.ListInitiativeWorkItemViews(ctx, store.DB(), home.WorkspaceKey, initiative.InitiativeKey)
		if err != nil {
			return workspacePayload{}, err
		}
		initiativeWorkItems = append(initiativeWorkItems, workItems...)
	}
	blocked, err := projections.ListWorkspaceBlockedItemViews(ctx, store.DB(), home.WorkspaceKey)
	if err != nil {
		return workspacePayload{}, err
	}
	approvals, err := projections.ListWorkspacePendingApprovalViews(ctx, store.DB(), home.WorkspaceKey)
	if err != nil {
		return workspacePayload{}, err
	}

	payload := workspacePayload{
		Workspace: workspaceSummaryPayload{
			Key:                  home.WorkspaceKey,
			Name:                 home.WorkspaceName,
			InitiativeCount:      home.InitiativeCount,
			CompanionCount:       home.CompanionCount,
			PendingApprovalCount: home.PendingApprovalCount,
			BlockedItemCount:     home.BlockedItemCount,
		},
		Initiatives:         make([]initiativePayload, 0, len(initiatives)),
		InitiativeWorkItems: make([]initiativeWorkItemPayload, 0, len(initiativeWorkItems)),
		BlockedItems:        make([]blockedItemPayload, 0, len(blocked)),
		PendingApprovals:    make([]pendingApprovalPayload, 0, len(approvals)),
	}

	for _, initiative := range initiatives {
		payload.Initiatives = append(payload.Initiatives, initiativePayload{
			Key:               initiative.InitiativeKey,
			Title:             initiative.Title,
			Kind:              initiative.Kind,
			Status:            initiative.Status,
			ProjectKey:        initiative.ProjectKey,
			OwnerCompanionKey: initiative.OwnerCompanionKey,
			OpenWorkItemCount: initiative.OpenWorkItemCount,
		})
	}
	for _, item := range blocked {
		payload.BlockedItems = append(payload.BlockedItems, blockedItemPayload{
			TaskKey:       item.TaskKey,
			ProjectKey:    item.ProjectKey,
			InitiativeKey: item.InitiativeKey,
			CompanionKey:  item.CompanionKey,
			Reason:        item.Reason,
			NextStep:      item.NextStep,
		})
	}
	for _, item := range initiativeWorkItems {
		payload.InitiativeWorkItems = append(payload.InitiativeWorkItems, initiativeWorkItemPayload{
			InitiativeKey: item.InitiativeKey,
			TaskKey:       item.TaskKey,
			ProjectKey:    item.ProjectKey,
			Title:         item.Title,
			Status:        item.Status,
		})
	}
	for _, approval := range approvals {
		payload.PendingApprovals = append(payload.PendingApprovals, pendingApprovalPayload{
			ProjectKey: approval.ProjectKey,
			TaskKey:    approval.TaskKey,
			Status:     approval.Status,
		})
	}

	return payload, nil
}

func findWorkspaceHome(ctx context.Context, store *sqlite.Store, requestedWorkspaceKey string) (projections.WorkspaceHomeView, error) {
	homes, err := projections.ListWorkspaceHomeViews(ctx, store.DB())
	if err != nil {
		return projections.WorkspaceHomeView{}, err
	}
	if len(homes) == 0 {
		return projections.WorkspaceHomeView{}, errWorkspaceNotFound
	}
	if requestedWorkspaceKey == "" {
		return homes[0], nil
	}
	for _, home := range homes {
		if home.WorkspaceKey == requestedWorkspaceKey {
			return home, nil
		}
	}
	return projections.WorkspaceHomeView{}, errWorkspaceNotFound
}
