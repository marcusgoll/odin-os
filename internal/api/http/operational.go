package httpapi

import (
	"database/sql"
	"encoding/json"
	"net/http"

	"odin-os/internal/core/workspaces"
	healthsvc "odin-os/internal/runtime/health"
	"odin-os/internal/runtime/projections"
	metricsvc "odin-os/internal/telemetry/metrics"
)

type Dependencies struct {
	Health          healthsvc.Service
	Metrics         metricsvc.Service
	ReadModels      projections.Queryer
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
		report, ready, err := deps.Health.Readiness(request.Context(), deps.RegistryHealthy)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusServiceUnavailable)
			return
		}

		statusCode := http.StatusOK
		if !ready {
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
		if deps.ReadModels == nil {
			http.Error(writer, "read models unavailable", http.StatusServiceUnavailable)
			return
		}
		view, err := projections.GetWorkspaceOverviewView(request.Context(), deps.ReadModels, workspaces.DefaultWorkspaceKey)
		if err != nil {
			if err == sql.ErrNoRows {
				http.NotFound(writer, request)
				return
			}
			http.Error(writer, err.Error(), http.StatusServiceUnavailable)
			return
		}
		writeJSON(writer, http.StatusOK, view)
	})
	mux.HandleFunc("/initiatives", func(writer http.ResponseWriter, request *http.Request) {
		if deps.ReadModels == nil {
			http.Error(writer, "read models unavailable", http.StatusServiceUnavailable)
			return
		}
		views, err := projections.ListInitiativePortfolioViews(request.Context(), deps.ReadModels, workspaces.DefaultWorkspaceKey)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusServiceUnavailable)
			return
		}
		writeJSON(writer, http.StatusOK, views)
	})
	mux.HandleFunc("/companions", func(writer http.ResponseWriter, request *http.Request) {
		if deps.ReadModels == nil {
			http.Error(writer, "read models unavailable", http.StatusServiceUnavailable)
			return
		}
		views, err := projections.ListCompanionAssignmentViews(request.Context(), deps.ReadModels, workspaces.DefaultWorkspaceKey)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusServiceUnavailable)
			return
		}
		writeJSON(writer, http.StatusOK, views)
	})
	mux.HandleFunc("/blocked", func(writer http.ResponseWriter, request *http.Request) {
		if deps.ReadModels == nil {
			http.Error(writer, "read models unavailable", http.StatusServiceUnavailable)
			return
		}
		views, err := projections.ListBlockedItemViews(request.Context(), deps.ReadModels)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusServiceUnavailable)
			return
		}
		writeJSON(writer, http.StatusOK, views)
	})
	return mux
}

func writeJSON(writer http.ResponseWriter, statusCode int, payload any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(statusCode)
	_ = json.NewEncoder(writer).Encode(payload)
}
