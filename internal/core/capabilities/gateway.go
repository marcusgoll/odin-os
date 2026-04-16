package capabilities

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"odin-os/internal/registry"
	"odin-os/internal/runtime/runs"
)

var errCapabilityNotFound = errors.New("capability not found")
var errCapabilityVersionRequired = errors.New("capability version is required")
var errCapabilityVersionMismatch = errors.New("capability version mismatch")
var errCapabilityDispatcherMissing = errors.New("capability dispatcher is not configured")
var errRunLookupMissing = errors.New("run lookup is not configured")
var errInvalidInvokeInput = errors.New("invalid capability input")
var errNotImplemented = errors.New("not implemented")

type InvokerFunc func(context.Context, InvokeRequest, Descriptor) (InvokeResponse, error)

type Gateway struct {
	snapshot func() Snapshot
	invoke   InvokerFunc
	runs     RunLookup
}

type SnapshotSource interface {
	Active() Snapshot
}

type RunLookup interface {
	GetRun(context.Context, int64) (runs.RunRecord, error)
}

func NewGateway(snapshot SnapshotSource, invoker InvokerFunc, runs RunLookup) *Gateway {
	gateway := &Gateway{
		invoke: invoker,
		runs:   runs,
	}
	if snapshot != nil {
		gateway.snapshot = snapshot.Active
	}
	return gateway
}

func (gateway *Gateway) ListCapabilities(kind registry.Kind, scope string) []CapabilityCard {
	if gateway == nil || gateway.snapshot == nil {
		return nil
	}

	snapshot := gateway.snapshot()
	cards := make([]CapabilityCard, 0, len(snapshot.Capabilities))
	for _, descriptor := range snapshot.Capabilities {
		if kind != registry.KindUnknown && descriptor.Kind != kind {
			continue
		}
		if !matchesCapabilityScope(descriptor, scope) {
			continue
		}
		cards = append(cards, capabilityCard(descriptor))
	}

	sort.Slice(cards, func(i, j int) bool {
		if cards[i].ID != cards[j].ID {
			return cards[i].ID < cards[j].ID
		}
		return cards[i].Version < cards[j].Version
	})

	return cards
}

func (gateway *Gateway) GetCapability(id, version string) (Descriptor, error) {
	descriptor, err := gateway.lookupCapability(id, version)
	if err != nil {
		return Descriptor{}, err
	}
	return descriptor, nil
}

func (gateway *Gateway) InvokeCapability(ctx context.Context, request InvokeRequest) (InvokeResponse, error) {
	descriptor, err := gateway.lookupCapability(request.CapabilityID, request.CapabilityVersion)
	if err != nil {
		return InvokeResponse{}, err
	}

	if err := validateInvokeInput(descriptor, request.Input); err != nil {
		return InvokeResponse{}, err
	}
	if gateway == nil || gateway.invoke == nil {
		return InvokeResponse{}, errCapabilityDispatcherMissing
	}

	return gateway.invoke(ctx, request, descriptor)
}

func (gateway *Gateway) GetRun(ctx context.Context, runID int64) (InvokeResponse, error) {
	if gateway == nil || gateway.runs == nil {
		return InvokeResponse{}, errRunLookupMissing
	}

	record, err := gateway.runs.GetRun(ctx, runID)
	if err != nil {
		return InvokeResponse{}, err
	}

	response := InvokeResponse{
		RunID:     strconv.FormatInt(record.RunID, 10),
		Status:    record.Status,
		Artifacts: []Artifact{},
	}
	return response, nil
}

func (gateway *Gateway) ResumeRun(context.Context, int64) error {
	return errNotImplemented
}

func (gateway *Gateway) CancelRun(context.Context, int64) error {
	return errNotImplemented
}

func (gateway *Gateway) lookupCapability(id, version string) (Descriptor, error) {
	if gateway == nil || gateway.snapshot == nil {
		return Descriptor{}, errCapabilityNotFound
	}
	if strings.TrimSpace(version) == "" {
		return Descriptor{}, errCapabilityVersionRequired
	}

	snapshot := gateway.snapshot()
	for _, descriptor := range snapshot.Capabilities {
		if descriptor.Key != id {
			continue
		}
		if version != "" && descriptor.Version != version {
			return Descriptor{}, errCapabilityVersionMismatch
		}
		return cloneDescriptor(descriptor), nil
	}

	return Descriptor{}, errCapabilityNotFound
}

func matchesCapabilityScope(descriptor Descriptor, scope string) bool {
	if scope == "" {
		return true
	}
	if descriptor.Availability.Scope == scope {
		return true
	}
	for _, candidate := range descriptor.Scopes {
		if candidate == scope {
			return true
		}
	}
	return false
}

func capabilityCard(descriptor Descriptor) CapabilityCard {
	return CapabilityCard{
		ID:      descriptor.Key,
		Kind:    descriptor.Kind,
		Name:    descriptor.Name,
		Title:   descriptor.Title,
		Version: descriptor.Version,
		Scope:   descriptor.Availability.Scope,
		Summary: descriptor.Summary,
		Status:  descriptor.Status,
	}
}

func validateInvokeInput(descriptor Descriptor, input json.RawMessage) error {
	if descriptor.InputSchema.Type != "object" {
		return nil
	}

	var payload any
	if err := json.Unmarshal(input, &payload); err != nil {
		return fmt.Errorf("%w: %v", errInvalidInvokeInput, err)
	}
	if _, ok := payload.(map[string]any); !ok {
		return fmt.Errorf("%w: expected JSON object input", errInvalidInvokeInput)
	}
	if strings.TrimSpace(string(input)) == "" {
		return fmt.Errorf("%w: input is required", errInvalidInvokeInput)
	}
	return nil
}
