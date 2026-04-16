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

type ManualWatcher struct {
	events chan Event
	mu     sync.Mutex
	closed bool
	once   sync.Once
}

func NewManual(buffer int) *ManualWatcher {
	if buffer < 0 {
		buffer = 0
	}
	return &ManualWatcher{
		events: make(chan Event, buffer),
	}
}

func (watcher *ManualWatcher) Events() <-chan Event {
	return watcher.events
}

func (watcher *ManualWatcher) Send(event Event) bool {
	watcher.mu.Lock()
	defer watcher.mu.Unlock()
	if watcher.closed {
		return false
	}
	select {
	case watcher.events <- event:
		return true
	default:
		return false
	}
}

func (watcher *ManualWatcher) Close() error {
	watcher.once.Do(func() {
		watcher.mu.Lock()
		watcher.closed = true
		close(watcher.events)
		watcher.mu.Unlock()
	})
	return nil
}
