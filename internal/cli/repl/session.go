package repl

import (
	"odin-os/internal/cli/scope"
	clistate "odin-os/internal/cli/state"
	"odin-os/internal/core/projects"
)

type Mode = clistate.Mode
type Cache = clistate.Cache
type SessionStore = clistate.SessionStore
type State = clistate.State

const (
	ModeAsk = clistate.ModeAsk
	ModeAct = clistate.ModeAct
)

func ResolveStartupState(cache Cache, registry projects.Registry) State {
	return clistate.ResolveStartupState(cache, registry)
}

func sanitizeMode(mode Mode, resolved scope.Resolution) Mode {
	return clistate.SanitizeMode(mode, resolved)
}
