package broker

import (
	"path/filepath"

	"odin-os/internal/registry"
	registryloader "odin-os/internal/registry/loader"
)

type SnapshotSource interface {
	LoadSnapshot() (registry.Snapshot, error)
}

type staticSource struct {
	snapshot registry.Snapshot
}

func StaticSource(snapshot registry.Snapshot) SnapshotSource {
	return staticSource{snapshot: normalizeSnapshot(snapshot)}
}

func (source staticSource) LoadSnapshot() (registry.Snapshot, error) {
	return source.snapshot, nil
}

type RegistryLoaderSource struct {
	RepoRoot string
}

func (source RegistryLoaderSource) LoadSnapshot() (registry.Snapshot, error) {
	return registryloader.LoadDir(filepath.Join(source.RepoRoot, "registry"))
}
