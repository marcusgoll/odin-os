package watcher

import "sync"

type EventType string

const (
	EventAdded    EventType = "added"
	EventModified EventType = "modified"
	EventRemoved  EventType = "removed"
)

type Event struct {
	Type EventType
	Path string
}

type Watcher interface {
	Events() <-chan Event
	Close() error
}

type NoopWatcher struct {
	events chan Event
	once   sync.Once
}

func NewNoop() *NoopWatcher {
	return &NoopWatcher{
		events: make(chan Event),
	}
}

func (watcher *NoopWatcher) Events() <-chan Event {
	return watcher.events
}

func (watcher *NoopWatcher) Close() error {
	watcher.once.Do(func() {
		close(watcher.events)
	})
	return nil
}
