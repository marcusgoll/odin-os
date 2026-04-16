package capabilities

import (
	"errors"

	"odin-os/internal/registry"
)

var errMissingSnapshotDigest = errors.New("capabilities snapshot digest is required")

var errMissingCapabilities = errors.New("capabilities snapshot capabilities are required")

type Service struct {
	active Snapshot
}

func NewService(snapshot Snapshot) (*Service, error) {
	if snapshot.Digest == "" {
		return nil, errMissingSnapshotDigest
	}
	if snapshot.Capabilities == nil {
		return nil, errMissingCapabilities
	}

	return &Service{
		active: cloneSnapshot(snapshot),
	}, nil
}

func (s *Service) Active() Snapshot {
	if s == nil {
		return Snapshot{}
	}
	return cloneSnapshot(s.active)
}

func cloneSnapshot(snapshot Snapshot) Snapshot {
	cloned := Snapshot{
		Digest:       snapshot.Digest,
		Diagnostics:  cloneDiagnostics(snapshot.Diagnostics),
		Capabilities: make(map[string]Descriptor, len(snapshot.Capabilities)),
	}
	for key, descriptor := range snapshot.Capabilities {
		cloned.Capabilities[key] = cloneDescriptor(descriptor)
	}
	return cloned
}

func cloneDescriptor(descriptor Descriptor) Descriptor {
	cloned := descriptor
	cloned.Tags = append([]string(nil), descriptor.Tags...)
	cloned.Owners = append([]string(nil), descriptor.Owners...)
	cloned.Scopes = append([]string(nil), descriptor.Scopes...)
	cloned.Tools = append([]string(nil), descriptor.Tools...)
	cloned.AppliesTo = append([]string(nil), descriptor.AppliesTo...)
	cloned.Composes = append([]string(nil), descriptor.Composes...)
	cloned.Aliases = append([]string(nil), descriptor.Aliases...)
	cloned.Sections = cloneSections(descriptor.Sections)
	return cloned
}

func cloneDiagnostics(diagnostics []registry.Diagnostic) []registry.Diagnostic {
	return append([]registry.Diagnostic(nil), diagnostics...)
}

func cloneSections(sections map[string]string) map[string]string {
	if sections == nil {
		return nil
	}
	cloned := make(map[string]string, len(sections))
	for key, value := range sections {
		cloned[key] = value
	}
	return cloned
}
