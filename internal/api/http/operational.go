package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"

	healthsvc "odin-os/internal/runtime/health"
	"odin-os/internal/runtime/projections"
	metricsvc "odin-os/internal/telemetry/metrics"
)

const defaultWorkspaceKey = "marcus"

type Dependencies struct {
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
	mux.HandleFunc("/memoryz", func(writer http.ResponseWriter, request *http.Request) {
		if deps.Health.DB == nil {
			http.Error(writer, "database handle is not configured", http.StatusServiceUnavailable)
			return
		}

		workspaceID, err := lookupWorkspaceIDByKey(request.Context(), deps.Health.DB, defaultWorkspaceKey)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusServiceUnavailable)
			return
		}
		if workspaceID == nil {
			writeJSON(writer, http.StatusOK, struct {
				Workspace   []projections.WorkspaceMemoryView  `json:"workspace"`
				Initiatives []projections.InitiativeMemoryView `json:"initiatives"`
				Companions  []projections.CompanionMemoryView  `json:"companions"`
			}{})
			return
		}

		workspaceViews, err := projections.ListWorkspaceMemoryViews(request.Context(), deps.Health.DB, projections.WorkspaceMemoryQuery{
			WorkspaceID: workspaceID,
			Limit:       1,
		})
		if err != nil {
			http.Error(writer, err.Error(), http.StatusServiceUnavailable)
			return
		}

		initiativeViews, err := projections.ListInitiativeMemoryViews(request.Context(), deps.Health.DB, projections.InitiativeMemoryQuery{
			WorkspaceID: workspaceID,
			Limit:       10,
		})
		if err != nil {
			http.Error(writer, err.Error(), http.StatusServiceUnavailable)
			return
		}
		companionViews, err := projections.ListCompanionMemoryViews(request.Context(), deps.Health.DB, projections.CompanionMemoryQuery{
			WorkspaceID: workspaceID,
			Limit:       10,
		})
		if err != nil {
			http.Error(writer, err.Error(), http.StatusServiceUnavailable)
			return
		}

		writeJSON(writer, http.StatusOK, struct {
			Workspace   []projections.WorkspaceMemoryView  `json:"workspace"`
			Initiatives []projections.InitiativeMemoryView `json:"initiatives"`
			Companions  []projections.CompanionMemoryView  `json:"companions"`
		}{
			Workspace:   workspaceViews,
			Initiatives: initiativeViews,
			Companions:  companionViews,
		})
	})
	return mux
}

func writeJSON(writer http.ResponseWriter, statusCode int, payload any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(statusCode)
	_ = json.NewEncoder(writer).Encode(payload)
}

func lookupWorkspaceIDByKey(ctx context.Context, db *sql.DB, key string) (*int64, error) {
	var workspaceID int64
	err := db.QueryRowContext(ctx, `SELECT id FROM workspaces WHERE key = ?`, key).Scan(&workspaceID)
	switch err {
	case nil:
		return &workspaceID, nil
	case sql.ErrNoRows:
		return nil, nil
	default:
		return nil, err
	}
}
