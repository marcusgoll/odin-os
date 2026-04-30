package config

// RuntimeConfig contains the scaffold-level service controls shared by the
// agency daemon and tests. Production loading remains owned by existing Odin
// config packages until the agency contract is implemented.
type RuntimeConfig struct {
	DryRun        bool
	KillSwitch    bool
	WorkspaceRoot string
	LogDir        string
}

// Default returns safe local defaults for an agency scaffold.
func Default() RuntimeConfig {
	return RuntimeConfig{
		DryRun:        true,
		KillSwitch:    false,
		WorkspaceRoot: "workspaces",
		LogDir:        "logs",
	}
}
