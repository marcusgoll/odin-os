package repl

import (
	"context"

	clistate "odin-os/internal/cli/state"
	"odin-os/internal/core/capabilities"
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

var ResolveStartupState = clistate.ResolveStartupState
var sanitizeMode = clistate.SanitizeMode

type capabilityGateway interface {
	ListCapabilities(kind registry.Kind, scope string) []capabilities.CapabilityCard
	GetCapability(id, version string) (capabilities.Descriptor, error)
	InvokeCapability(context.Context, capabilities.InvokeRequest) (capabilities.InvokeResponse, error)
	GetRun(context.Context, int64) (capabilities.RunEnvelope, error)
}
