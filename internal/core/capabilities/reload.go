package capabilities

import (
	"context"
	"errors"

	runtimeevents "odin-os/internal/runtime/events"
)

var errMissingSnapshotLoader = errors.New("capabilities snapshot loader is required")
var errNilService = errors.New("capabilities service is nil")
var errStaleReloadSnapshot = errors.New("capabilities snapshot changed during reload")

type LoaderFunc func(context.Context) (Snapshot, error)

type EventSink func(runtimeevents.Type, any)

type Option func(*Service)

func WithLoader(loader LoaderFunc) Option {
	return func(service *Service) {
		service.loader = loader
	}
}

func WithEventSink(sink EventSink) Option {
	return func(service *Service) {
		service.sink = sink
	}
}

func noopEventSink(runtimeevents.Type, any) {}

func (s *Service) Publish(next Snapshot) error {
	if s == nil {
		return errNilService
	}

	if err := validateSnapshot(next); err != nil {
		s.emitSnapshotRejected(s.currentSnapshot(), next, err)
		return err
	}

	s.mu.Lock()
	previous := cloneSnapshot(s.active)
	cloned := cloneSnapshot(next)
	s.active = cloned
	sink := s.sink
	s.mu.Unlock()

	if sink == nil {
		sink = noopEventSink
	}
	sink(runtimeevents.EventCapabilitySnapshotPublished, runtimeevents.CapabilitySnapshotPublishedPayload{
		PreviousDigest:  previous.Digest,
		Digest:          cloned.Digest,
		CapabilityCount: len(cloned.Capabilities),
	})
	return nil
}

func (s *Service) Reload(ctx context.Context) (Snapshot, error) {
	if s == nil {
		return Snapshot{}, errNilService
	}
	if ctx == nil {
		ctx = context.Background()
	}

	loader := s.loader
	if loader == nil {
		err := errMissingSnapshotLoader
		s.emitSnapshotRejected(s.currentSnapshot(), Snapshot{}, err)
		return Snapshot{}, err
	}

	expectedDigest := s.activeDigest()
	next, err := loader(ctx)
	if err != nil {
		s.emitSnapshotRejected(s.currentSnapshot(), next, err)
		return Snapshot{}, err
	}

	published, err := s.publishIfCurrent(expectedDigest, next)
	if err != nil {
		return Snapshot{}, err
	}

	return published, nil
}

func (s *Service) publishIfCurrent(expectedDigest string, next Snapshot) (Snapshot, error) {
	if s == nil {
		return Snapshot{}, errNilService
	}

	if err := validateSnapshot(next); err != nil {
		s.emitSnapshotRejected(s.currentSnapshot(), next, err)
		return Snapshot{}, err
	}

	s.mu.Lock()
	previous := cloneSnapshot(s.active)
	if previous.Digest != expectedDigest {
		sink := s.sink
		s.mu.Unlock()
		if sink == nil {
			sink = noopEventSink
		}
		sink(runtimeevents.EventCapabilitySnapshotRejected, runtimeevents.CapabilitySnapshotRejectedPayload{
			PreviousDigest:  previous.Digest,
			Digest:          next.Digest,
			CapabilityCount: len(next.Capabilities),
			Reason:          errStaleReloadSnapshot.Error(),
		})
		return Snapshot{}, errStaleReloadSnapshot
	}
	cloned := cloneSnapshot(next)
	s.active = cloned
	sink := s.sink
	s.mu.Unlock()

	if sink == nil {
		sink = noopEventSink
	}
	sink(runtimeevents.EventCapabilitySnapshotPublished, runtimeevents.CapabilitySnapshotPublishedPayload{
		PreviousDigest:  previous.Digest,
		Digest:          cloned.Digest,
		CapabilityCount: len(cloned.Capabilities),
	})
	return cloned, nil
}

func (s *Service) emitSnapshotRejected(previous Snapshot, next Snapshot, err error) {
	if s == nil {
		return
	}

	sink := s.sink
	if sink == nil {
		sink = noopEventSink
	}

	sink(runtimeevents.EventCapabilitySnapshotRejected, runtimeevents.CapabilitySnapshotRejectedPayload{
		PreviousDigest:  previous.Digest,
		Digest:          next.Digest,
		CapabilityCount: len(next.Capabilities),
		Reason:          err.Error(),
	})
}
