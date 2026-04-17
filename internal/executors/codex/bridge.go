package codex

import (
	"encoding/json"

	"odin-os/internal/core/capabilities"
)

type ProviderCall struct {
	RequestID         string
	CapabilityID      string
	CapabilityVersion string
	Scope             capabilities.ScopeRef
	Caller            capabilities.CallerRef
	Input             json.RawMessage
	Execution         capabilities.ExecutionRequest
	ProviderPrompt    string
}

type ProviderResult struct {
	RunID     string
	Status    string
	Output    json.RawMessage
	Artifacts []capabilities.Artifact
	Error     *capabilities.RunError
}

type Bridge interface {
	ToInvokeRequest(providerCall ProviderCall) (capabilities.InvokeRequest, error)
	FromInvokeResponse(resp capabilities.InvokeResponse) (ProviderResult, error)
}

type bridge struct{}

func NewBridge() Bridge {
	return bridge{}
}

func (bridge) ToInvokeRequest(providerCall ProviderCall) (capabilities.InvokeRequest, error) {
	return capabilities.InvokeRequest{
		RequestID:         providerCall.RequestID,
		CapabilityID:      providerCall.CapabilityID,
		CapabilityVersion: providerCall.CapabilityVersion,
		Scope:             providerCall.Scope,
		Caller:            providerCall.Caller,
		Input:             append(json.RawMessage(nil), providerCall.Input...),
		Execution:         providerCall.Execution,
	}, nil
}

func (bridge) FromInvokeResponse(resp capabilities.InvokeResponse) (ProviderResult, error) {
	return ProviderResult{
		RunID:     resp.RunID,
		Status:    resp.Status,
		Output:    append(json.RawMessage(nil), resp.Output...),
		Artifacts: append([]capabilities.Artifact(nil), resp.Artifacts...),
		Error:     resp.Error,
	}, nil
}
