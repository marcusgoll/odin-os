package capabilities

import "odin-os/internal/registry"

type Descriptor = registry.Item

type Snapshot struct {
	Digest       string
	Diagnostics  []registry.Diagnostic
	Capabilities map[string]Descriptor
}
