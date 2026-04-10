package repl

import clistate "odin-os/internal/cli/state"

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
