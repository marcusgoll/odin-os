package mcp

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"odin-os/internal/core/capabilities"
	"odin-os/internal/registry"
)

type CapabilitySource interface {
	ListCapabilities(kind registry.Kind, scope string) []capabilities.CapabilityCard
	GetCapability(id, version string) (capabilities.Descriptor, error)
}

type Tool struct {
	CapabilityID      string
	CapabilityVersion string
	Kind              registry.Kind
	Name              string
	Scope             string
	Description       string
	Permissions       []string
	InputSchema       registry.SchemaRef
	OutputSchema      registry.SchemaRef
}

type Server struct {
	source CapabilitySource
}

func NewServer(source CapabilitySource) *Server {
	return &Server{source: source}
}

func (server *Server) ListTools(ctx context.Context, scope string) ([]Tool, error) {
	_ = ctx
	if server == nil || server.source == nil {
		return nil, fmt.Errorf("mcp capability source is not configured")
	}

	tools := make([]Tool, 0)
	for _, kind := range []registry.Kind{registry.KindSkill, registry.KindWorkflow, registry.KindCommand} {
		for _, card := range server.source.ListCapabilities(kind, scope) {
			descriptor, err := server.source.GetCapability(card.ID, card.Version)
			if err != nil {
				if isDescriptorUnavailable(err) {
					continue
				}
				return nil, err
			}
			if !descriptor.Kind.IsInvokable() {
				continue
			}
			tools = append(tools, Tool{
				CapabilityID:      descriptor.Key,
				CapabilityVersion: descriptor.Version,
				Kind:              descriptor.Kind,
				Name:              descriptor.Name,
				Scope:             descriptor.Availability.Scope,
				Description:       descriptor.Summary,
				Permissions:       append([]string(nil), descriptor.Permissions...),
				InputSchema:       descriptor.InputSchema,
				OutputSchema:      descriptor.OutputSchema,
			})
		}
	}

	sort.Slice(tools, func(i, j int) bool {
		if tools[i].CapabilityID != tools[j].CapabilityID {
			return tools[i].CapabilityID < tools[j].CapabilityID
		}
		return tools[i].CapabilityVersion < tools[j].CapabilityVersion
	})

	return tools, nil
}

func isDescriptorUnavailable(err error) bool {
	if err == nil {
		return false
	}

	type coder interface {
		Code() string
	}
	var coded coder
	if errors.As(err, &coded) && coded.Code() == "not_found" {
		return true
	}
	return err.Error() == "capability not found"
}
