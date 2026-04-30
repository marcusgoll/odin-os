package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	httpapi "odin-os/internal/api/http"
	"odin-os/internal/core/capabilities"
	"odin-os/internal/registry"
)

func TestHTTPCapabilityEndpoints(t *testing.T) {
	t.Parallel()

	gateway := &recordingHTTPCapabilityGateway{
		cardsByScope: map[string][]capabilities.CapabilityCard{
			"project": {
				{ID: "project.status", Kind: registry.KindCommand, Name: "project.status", Version: "1.0.0", Scope: "project"},
			},
		},
		descriptor: capabilities.Descriptor{
			Kind:         registry.KindCommand,
			Key:          "project.status",
			Name:         "project.status",
			Version:      "1.0.0",
			Availability: registry.Availability{Scope: "project"},
			InputSchema:  registry.SchemaRef{Type: "object"},
			OutputSchema: registry.SchemaRef{Type: "object"},
		},
		invokeFn: func(_ context.Context, request capabilities.InvokeRequest, descriptor capabilities.Descriptor) (capabilities.InvokeResponse, error) {
			if bytes.Contains(request.Input, []byte(`"invalid":true`)) {
				return capabilities.InvokeResponse{
						RunID:  "run-9",
						Status: "failed",
						Error: &capabilities.RunError{
							Code:    "validation_failed",
							Message: "bad input",
						},
					}, &capabilities.Error{
						CodeValue: "validation_failed",
						Message:   "bad input",
					}
			}
			if bytes.Contains(request.Input, []byte(`"forbidden":true`)) {
				return capabilities.InvokeResponse{
						RunID:  "run-8",
						Status: "failed",
						Error: &capabilities.RunError{
							Code:    "permission_denied",
							Message: "policy denied",
						},
					}, &capabilities.Error{
						CodeValue: "permission_denied",
						Message:   "policy denied",
					}
			}
			return capabilities.InvokeResponse{
				RunID:  "run-7",
				Status: "completed",
				Output: json.RawMessage(`{"status":"ok"}`),
			}, nil
		},
		runEnvelope: capabilities.RunEnvelope{
			RunID:  "42",
			Status: "failed",
			Error: &capabilities.RunError{
				Code:    "run_failed",
				Message: "executor failed",
			},
		},
	}

	server := httptest.NewServer(httpapi.NewCapabilitiesHandler(httpapi.CapabilitiesDependencies{
		Gateway: gateway,
	}))
	defer server.Close()

	res := mustRequest(t, server, http.MethodGet, "/capabilities?scope=project", nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /capabilities status = %d, want %d", res.StatusCode, http.StatusOK)
	}
	var cards []struct {
		ID      string `json:"id"`
		Scope   string `json:"scope"`
		Kind    string `json:"kind"`
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	decodeJSON(t, res.Body, &cards)
	if len(cards) != 1 || cards[0].ID != "project.status" || cards[0].Scope != "project" {
		t.Fatalf("GET /capabilities response = %+v, want one project-scoped capability", cards)
	}

	res = mustRequest(t, server, http.MethodGet, "/capabilities/project.status", nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /capabilities/project.status status = %d, want %d", res.StatusCode, http.StatusOK)
	}
	var descriptor struct {
		ID           string `json:"id"`
		Version      string `json:"version"`
		Availability struct {
			Scope string `json:"scope"`
		} `json:"availability"`
	}
	decodeJSON(t, res.Body, &descriptor)
	if descriptor.ID != "project.status" || descriptor.Version != "1.0.0" || descriptor.Availability.Scope != "project" {
		t.Fatalf("GET /capabilities/project.status response = %+v, want project.status 1.0.0", descriptor)
	}

	res = mustRequest(t, server, http.MethodPost, "/capabilities/project.status:invoke", bytes.NewBufferString(`{"scope":{"kind":"project","project_key":"alpha"},"input":{"invalid":true}}`))
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("POST /capabilities/project.status:invoke status = %d, want %d", res.StatusCode, http.StatusBadRequest)
	}
	var invokeResponse struct {
		RunID  string `json:"run_id"`
		Status string `json:"status"`
		Error  struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	decodeJSON(t, res.Body, &invokeResponse)
	if invokeResponse.RunID != "run-9" || invokeResponse.Status != "failed" {
		t.Fatalf("invoke response = %+v, want run-9 failed", invokeResponse)
	}
	if invokeResponse.Error.Code != "validation_failed" {
		t.Fatalf("invoke error code = %q, want %q", invokeResponse.Error.Code, "validation_failed")
	}

	res = mustRequest(t, server, http.MethodPost, "/capabilities/project.status:invoke", bytes.NewBufferString(`{"scope":{"kind":"project","project_key":"alpha"},"input":{"forbidden":true}}`))
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("POST /capabilities/project.status:invoke forbidden status = %d, want %d", res.StatusCode, http.StatusForbidden)
	}
	decodeJSON(t, res.Body, &invokeResponse)
	if invokeResponse.Error.Code != "permission_denied" {
		t.Fatalf("invoke forbidden error code = %q, want %q", invokeResponse.Error.Code, "permission_denied")
	}

	res = mustRequest(t, server, http.MethodGet, "/runs/42", nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /runs/42 status = %d, want %d", res.StatusCode, http.StatusOK)
	}
	var runResponse struct {
		RunID  string `json:"run_id"`
		Status string `json:"status"`
		Error  struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	decodeJSON(t, res.Body, &runResponse)
	if runResponse.RunID != "42" || runResponse.Status != "failed" {
		t.Fatalf("run response = %+v, want run 42 failed", runResponse)
	}
	if runResponse.Error.Code != "run_failed" {
		t.Fatalf("run error code = %q, want %q", runResponse.Error.Code, "run_failed")
	}
}

func TestCapabilitiesHandlerFallsBackForRunsWhenGatewayIsUnavailable(t *testing.T) {
	t.Parallel()

	fallback := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/runs/42" {
			t.Fatalf("fallback received %s %s, want GET /runs/42", request.Method, request.URL.Path)
		}
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte(`{"id":42,"status":"running"}`))
	})
	server := httptest.NewServer(httpapi.NewCapabilitiesHandler(httpapi.CapabilitiesDependencies{
		Fallback: fallback,
	}))
	defer server.Close()

	res := mustRequest(t, server, http.MethodGet, "/runs/42", nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /runs/42 status = %d, want %d", res.StatusCode, http.StatusOK)
	}
	var response struct {
		ID     int64  `json:"id"`
		Status string `json:"status"`
	}
	decodeJSON(t, res.Body, &response)
	if response.ID != 42 || response.Status != "running" {
		t.Fatalf("fallback run response = %+v, want running run 42", response)
	}
}

func TestHTTPCapabilityEndpointsRequireVersionForAmbiguousCapability(t *testing.T) {
	t.Parallel()

	gateway := &recordingHTTPCapabilityGateway{
		cardsByScope: map[string][]capabilities.CapabilityCard{
			"": {
				{ID: "project.status", Kind: registry.KindCommand, Name: "project.status", Version: "1.2.0", Scope: "project"},
				{ID: "project.status", Kind: registry.KindCommand, Name: "project.status", Version: "1.10.0", Scope: "project"},
			},
		},
	}

	server := httptest.NewServer(httpapi.NewCapabilitiesHandler(httpapi.CapabilitiesDependencies{
		Gateway: gateway,
	}))
	defer server.Close()

	res := mustRequest(t, server, http.MethodGet, "/capabilities/project.status", nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("GET /capabilities/project.status ambiguous status = %d, want %d", res.StatusCode, http.StatusBadRequest)
	}

	var payload struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	decodeJSON(t, res.Body, &payload)
	if payload.Error.Code != "version_required" {
		t.Fatalf("ambiguous capability error code = %q, want %q", payload.Error.Code, "version_required")
	}
}

func TestHTTPCapabilityEndpointsMapVersionMismatch(t *testing.T) {
	t.Parallel()

	gateway := &recordingHTTPCapabilityGateway{
		getErr: errors.New("capability version mismatch"),
	}

	server := httptest.NewServer(httpapi.NewCapabilitiesHandler(httpapi.CapabilitiesDependencies{
		Gateway: gateway,
	}))
	defer server.Close()

	res := mustRequest(t, server, http.MethodGet, "/capabilities/project.status?version=9.9.9", nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("GET /capabilities/project.status?version=9.9.9 status = %d, want %d", res.StatusCode, http.StatusBadRequest)
	}

	var payload struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	decodeJSON(t, res.Body, &payload)
	if payload.Error.Code != "version_mismatch" {
		t.Fatalf("version mismatch error code = %q, want %q", payload.Error.Code, "version_mismatch")
	}
}

type recordingHTTPCapabilityGateway struct {
	cardsByScope map[string][]capabilities.CapabilityCard
	descriptor   capabilities.Descriptor
	getErr       error
	invokeFn     func(context.Context, capabilities.InvokeRequest, capabilities.Descriptor) (capabilities.InvokeResponse, error)
	runEnvelope  capabilities.RunEnvelope
}

func (gateway *recordingHTTPCapabilityGateway) ListCapabilities(kind registry.Kind, scope string) []capabilities.CapabilityCard {
	if scope == "" {
		var cards []capabilities.CapabilityCard
		for _, scoped := range gateway.cardsByScope {
			cards = append(cards, scoped...)
		}
		return cards
	}
	if cards, ok := gateway.cardsByScope[scope]; ok {
		return append([]capabilities.CapabilityCard(nil), cards...)
	}
	return nil
}

func (gateway *recordingHTTPCapabilityGateway) GetCapability(id, version string) (capabilities.Descriptor, error) {
	if gateway.getErr != nil {
		return capabilities.Descriptor{}, gateway.getErr
	}
	return gateway.descriptor, nil
}

func (gateway *recordingHTTPCapabilityGateway) InvokeCapability(ctx context.Context, request capabilities.InvokeRequest) (capabilities.InvokeResponse, error) {
	if gateway.invokeFn == nil {
		return capabilities.InvokeResponse{}, nil
	}
	return gateway.invokeFn(ctx, request, gateway.descriptor)
}

func (gateway *recordingHTTPCapabilityGateway) GetRun(context.Context, int64) (capabilities.RunEnvelope, error) {
	return gateway.runEnvelope, nil
}

func mustRequest(t *testing.T, server *httptest.Server, method, path string, body *bytes.Buffer) *http.Response {
	t.Helper()

	var reader io.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		reader = bytes.NewReader(body.Bytes())
	}
	req, err := http.NewRequest(method, server.URL+path, reader)
	if err != nil {
		t.Fatalf("NewRequest(%s %s) error = %v", method, path, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do(%s %s) error = %v", method, path, err)
	}
	return res
}

func decodeJSON(t *testing.T, body io.Reader, dest any) {
	t.Helper()

	if err := json.NewDecoder(body).Decode(dest); err != nil {
		t.Fatalf("DecodeJSON() error = %v", err)
	}
}
