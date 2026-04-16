package catalog

func BuiltinDefinitions() map[string]ToolDefinition {
	// The default operator-facing built-in catalog stays empty until a tool is
	// backed by a real runtime surface. Placeholder operational tools must not be
	// advertised as live capability.
	return map[string]ToolDefinition{}
}
