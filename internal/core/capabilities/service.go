package capabilities

import (
	"errors"
	"sync"

	"odin-os/internal/registry"
)

var errMissingSnapshotDigest = errors.New("capabilities snapshot digest is required")

var errMissingCapabilities = errors.New("capabilities snapshot capabilities are required")

type Service struct {
	mu     sync.RWMutex
	active Snapshot
	loader LoaderFunc
	sink   EventSink
}

func NewService(snapshot Snapshot, opts ...Option) (*Service, error) {
	if err := validateSnapshot(snapshot); err != nil {
		return nil, err
	}

	service := &Service{
		active: cloneSnapshot(snapshot),
		sink:   noopEventSink,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(service)
		}
	}
	if service.sink == nil {
		service.sink = noopEventSink
	}

	return service, nil
}

func (s *Service) Active() Snapshot {
	if s == nil {
		return Snapshot{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneSnapshot(s.active)
}

func (s *Service) activeDigest() string {
	if s == nil {
		return ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.active.Digest
}

func (s *Service) currentSnapshot() Snapshot {
	if s == nil {
		return Snapshot{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
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

func validateSnapshot(snapshot Snapshot) error {
	if snapshot.Digest == "" {
		return errMissingSnapshotDigest
	}
	if snapshot.Capabilities == nil {
		return errMissingCapabilities
	}
	return nil
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
