package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"odin-os/internal/core/capabilities"
	"odin-os/internal/registry"
)

type CapabilityGateway interface {
	ListCapabilities(kind registry.Kind, scope string) []capabilities.CapabilityCard
	GetCapability(id, version string) (capabilities.Descriptor, error)
	InvokeCapability(context.Context, capabilities.InvokeRequest) (capabilities.InvokeResponse, error)
	GetRun(context.Context, int64) (capabilities.RunEnvelope, error)
}

type CapabilitiesDependencies struct {
	Gateway  CapabilityGateway
	Fallback http.Handler
}

type capabilityDescriptorResponse struct {
	ID             string                     `json:"id"`
	Kind           registry.Kind              `json:"kind"`
	Name           string                     `json:"name"`
	Version        string                     `json:"version"`
	Availability   registry.Availability      `json:"availability"`
	Permissions    []string                   `json:"permissions,omitempty"`
	InputSchema    registry.SchemaRef         `json:"input_schema,omitempty"`
	OutputSchema   registry.SchemaRef         `json:"output_schema,omitempty"`
	Dependencies   []registry.DependencyRef   `json:"dependencies,omitempty"`
	Execution      registry.ExecutionPolicy   `json:"execution,omitempty"`
	Implementation registry.ImplementationRef `json:"implementation,omitempty"`
	Status         string                     `json:"status,omitempty"`
}

type capabilityCardResponse struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	Title   string `json:"title,omitempty"`
	Version string `json:"version"`
	Scope   string `json:"scope,omitempty"`
	Summary string `json:"summary,omitempty"`
	Status  string `json:"status,omitempty"`
}

type invokeRequestBody struct {
	Version   string                        `json:"version,omitempty"`
	Scope     capabilities.ScopeRef         `json:"scope,omitempty"`
	Caller    capabilities.CallerRef        `json:"caller,omitempty"`
	Input     json.RawMessage               `json:"input,omitempty"`
	Execution capabilities.ExecutionRequest `json:"execution,omitempty"`
}

type invokeResponseBody struct {
	RunID     string                  `json:"run_id,omitempty"`
	Status    string                  `json:"status,omitempty"`
	Output    json.RawMessage         `json:"output,omitempty"`
	Artifacts []capabilities.Artifact `json:"artifacts,omitempty"`
	Error     *capabilities.RunError  `json:"error,omitempty"`
}

func NewCapabilitiesHandler(deps CapabilitiesDependencies) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /capabilities", func(writer http.ResponseWriter, request *http.Request) {
		if deps.Gateway == nil {
			writeAPIError(writer, http.StatusServiceUnavailable, "gateway_unavailable", "capability gateway is unavailable")
			return
		}

		scopeFilter := strings.TrimSpace(request.URL.Query().Get("scope"))
		cards := deps.Gateway.ListCapabilities(registry.KindUnknown, scopeFilter)
		payload := make([]capabilityCardResponse, 0, len(cards))
		for _, card := range cards {
			payload = append(payload, capabilityCardResponse{
				ID:      card.ID,
				Kind:    string(card.Kind),
				Name:    card.Name,
				Title:   card.Title,
				Version: card.Version,
				Scope:   card.Scope,
				Summary: card.Summary,
				Status:  card.Status,
			})
		}
		writeJSON(writer, http.StatusOK, payload)
	})

	mux.HandleFunc("/capabilities/", func(writer http.ResponseWriter, request *http.Request) {
		if deps.Gateway == nil {
			writeAPIError(writer, http.StatusServiceUnavailable, "gateway_unavailable", "capability gateway is unavailable")
			return
		}

		id, invoke := parseCapabilityRoute(request.URL.Path)
		if id == "" {
			http.NotFound(writer, request)
			return
		}

		if invoke {
			if request.Method != http.MethodPost {
				http.NotFound(writer, request)
				return
			}
			handleInvokeCapability(writer, request, deps.Gateway, id)
			return
		}

		if request.Method != http.MethodGet {
			http.NotFound(writer, request)
			return
		}
		handleGetCapability(writer, request, deps.Gateway, id)
	})

	mux.HandleFunc("GET /runs/{run_id}", func(writer http.ResponseWriter, request *http.Request) {
		if deps.Gateway == nil {
			writeAPIError(writer, http.StatusServiceUnavailable, "gateway_unavailable", "capability gateway is unavailable")
			return
		}

		runID, err := strconv.ParseInt(request.PathValue("run_id"), 10, 64)
		if err != nil {
			writeAPIError(writer, http.StatusBadRequest, "invalid_run_id", "run id must be an integer")
			return
		}

		envelope, err := deps.Gateway.GetRun(request.Context(), runID)
		if err != nil {
			writeGatewayError(writer, err)
			return
		}
		writeJSON(writer, http.StatusOK, envelope)
	})

	if deps.Fallback != nil {
		mux.Handle("/", deps.Fallback)
	}

	return mux
}

func parseCapabilityRoute(path string) (id string, invoke bool) {
	const prefix = "/capabilities/"
	if !strings.HasPrefix(path, prefix) {
		return "", false
	}
	value := strings.TrimSpace(strings.TrimPrefix(path, prefix))
	if value == "" {
		return "", false
	}
	if strings.HasSuffix(value, ":invoke") {
		return strings.TrimSuffix(value, ":invoke"), true
	}
	return value, false
}

func handleGetCapability(writer http.ResponseWriter, request *http.Request, gateway CapabilityGateway, id string) {
	descriptor, err := resolveCapabilityDescriptor(request.Context(), gateway, id, request.URL.Query().Get("version"))
	if err != nil {
		writeGatewayError(writer, err)
		return
	}

	writeJSON(writer, http.StatusOK, capabilityDescriptorResponse{
		ID:             descriptor.Key,
		Kind:           descriptor.Kind,
		Name:           descriptor.Name,
		Version:        descriptor.Version,
		Availability:   descriptor.Availability,
		Permissions:    append([]string(nil), descriptor.Permissions...),
		InputSchema:    descriptor.InputSchema,
		OutputSchema:   descriptor.OutputSchema,
		Dependencies:   append([]registry.DependencyRef(nil), descriptor.Dependencies...),
		Execution:      descriptor.Execution,
		Implementation: descriptor.Implementation,
		Status:         descriptor.Status,
	})
}

func handleInvokeCapability(writer http.ResponseWriter, request *http.Request, gateway CapabilityGateway, id string) {
	var body invokeRequestBody
	if err := json.NewDecoder(request.Body).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
		writeAPIError(writer, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	descriptor, err := resolveCapabilityDescriptor(request.Context(), gateway, id, body.Version)
	if err != nil {
		writeGatewayError(writer, err)
		return
	}

	response, invokeErr := gateway.InvokeCapability(request.Context(), capabilities.InvokeRequest{
		RequestID:         request.Header.Get("X-Request-ID"),
		CapabilityID:      descriptor.Key,
		CapabilityVersion: descriptor.Version,
		Scope:             body.Scope,
		Caller:            body.Caller,
		Input:             body.Input,
		Execution:         body.Execution,
	})
	if invokeErr != nil || response.Error != nil {
		errorPayload := response.Error
		if errorPayload == nil {
			errorPayload = runErrorFrom(invokeErr)
		}
		if errorPayload == nil {
			errorPayload = &capabilities.RunError{Code: "internal_error", Message: "capability invocation failed"}
		}
		writeJSON(writer, statusCodeForErrorCode(errorPayload.Code), invokeResponseBody{
			RunID:     response.RunID,
			Status:    fallbackStatus(response.Status, "failed"),
			Output:    response.Output,
			Artifacts: append([]capabilities.Artifact(nil), response.Artifacts...),
			Error:     errorPayload,
		})
		return
	}

	writeJSON(writer, http.StatusOK, invokeResponseBody{
		RunID:     response.RunID,
		Status:    fallbackStatus(response.Status, "completed"),
		Output:    response.Output,
		Artifacts: append([]capabilities.Artifact(nil), response.Artifacts...),
		Error:     response.Error,
	})
}

func resolveCapabilityDescriptor(ctx context.Context, gateway CapabilityGateway, id, version string) (capabilities.Descriptor, error) {
	if strings.TrimSpace(version) != "" {
		descriptor, err := gateway.GetCapability(id, version)
		if err != nil {
			return capabilities.Descriptor{}, normalizeLookupError(id, version, err)
		}
		return descriptor, nil
	}

	var matches []capabilities.CapabilityCard
	cards := gateway.ListCapabilities(registry.KindUnknown, "")
	for _, card := range cards {
		if card.ID == id {
			matches = append(matches, card)
		}
	}

	switch len(matches) {
	case 0:
		return capabilities.Descriptor{}, &capabilities.Error{
			CodeValue: "not_found",
			Message:   fmt.Sprintf("capability %q not found", id),
		}
	case 1:
		return gateway.GetCapability(id, matches[0].Version)
	default:
		return capabilities.Descriptor{}, &capabilities.Error{
			CodeValue: "version_required",
			Message:   fmt.Sprintf("capability %q requires an explicit version", id),
		}
	}
}

func normalizeLookupError(id, version string, err error) error {
	if err == nil {
		return nil
	}

	switch err.Error() {
	case "capability not found":
		return &capabilities.Error{
			CodeValue: "not_found",
			Message:   fmt.Sprintf("capability %q not found", id),
			Cause:     err,
		}
	case "capability version mismatch":
		return &capabilities.Error{
			CodeValue: "version_mismatch",
			Message:   fmt.Sprintf("capability %q version %q does not match an active descriptor", id, version),
			Cause:     err,
		}
	case "capability version is required":
		return &capabilities.Error{
			CodeValue: "version_required",
			Message:   fmt.Sprintf("capability %q requires an explicit version", id),
			Cause:     err,
		}
	default:
		return err
	}
}

func statusCodeForErrorCode(code string) int {
	switch code {
	case "permission_denied":
		return http.StatusForbidden
	case "not_found":
		return http.StatusNotFound
	case "invalid_scope", "validation_failed", "invalid_request", "version_required", "version_mismatch":
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}

func fallbackStatus(status, fallback string) string {
	if strings.TrimSpace(status) == "" {
		return fallback
	}
	return status
}

func runErrorFrom(err error) *capabilities.RunError {
	if err == nil {
		return nil
	}
	type coder interface {
		Code() string
	}
	var coded coder
	if errors.As(err, &coded) {
		return &capabilities.RunError{
			Code:    coded.Code(),
			Message: err.Error(),
		}
	}
	return &capabilities.RunError{
		Code:    "internal_error",
		Message: err.Error(),
	}
}

func writeGatewayError(writer http.ResponseWriter, err error) {
	if err == nil {
		writeAPIError(writer, http.StatusInternalServerError, "internal_error", "unexpected gateway error")
		return
	}

	type coder interface {
		Code() string
	}
	var coded coder
	if errors.As(err, &coded) {
		writeAPIError(writer, statusCodeForErrorCode(coded.Code()), coded.Code(), err.Error())
		return
	}

	writeAPIError(writer, http.StatusInternalServerError, "internal_error", err.Error())
}

func writeAPIError(writer http.ResponseWriter, statusCode int, code, message string) {
	writeJSON(writer, statusCode, map[string]any{
		"error": map[string]string{
			"code":    code,
			"message": message,
		},
	})
}
