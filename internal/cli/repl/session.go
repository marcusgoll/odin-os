package repl

import (
	"context"

	"odin-os/internal/cli/scope"
	clistate "odin-os/internal/cli/state"
	"odin-os/internal/core/capabilities"
	"odin-os/internal/core/projects"
	"odin-os/internal/registry"
)

type Mode = clistate.Mode
type Cache = clistate.Cache
type SessionStore = clistate.SessionStore
type State = clistate.State

const (
	ModeAsk = clistate.ModeAsk
	ModeAct = clistate.ModeAct
)

type capabilityGateway interface {
	ListCapabilities(kind registry.Kind, scope string) []capabilities.CapabilityCard
	GetCapability(id, version string) (capabilities.Descriptor, error)
	InvokeCapability(context.Context, capabilities.InvokeRequest) (capabilities.InvokeResponse, error)
	GetRun(context.Context, int64) (capabilities.RunEnvelope, error)
}

func ResolveStartupState(cache Cache, registry projects.Registry) State {
	return clistate.ResolveStartupState(cache, registry)
}

func sanitizeMode(mode Mode, resolved scope.Resolution) Mode {
	return clistate.SanitizeMode(mode, resolved)
}
