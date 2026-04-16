package capabilities

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"odin-os/internal/registry"
	runtimeevents "odin-os/internal/runtime/events"
)

type capturedEvent struct {
	typ     runtimeevents.Type
	payload json.RawMessage
}

func TestPublishSwapsSnapshotAtomically(t *testing.T) {
	service := newReloadTestService(t, "digest-a", nil)
	next := testSnapshot("digest-b", "beta")

	if err := service.Publish(next); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	active := service.Active()
	if active.Digest != next.Digest {
		t.Fatalf("Active().Digest = %q, want %q", active.Digest, next.Digest)
	}
	if got := active.Capabilities["cap.beta"]; got.Title != "Beta" {
		t.Fatalf("Active().Capabilities[cap.beta] = %+v, want beta capability", got)
	}

	next.Diagnostics[0].Code = "mutated"
	next.Capabilities["cap.beta"] = registry.Item{Key: "cap.beta", Title: "Changed"}

	stillActive := service.Active()
	if stillActive.Diagnostics[0].Code != "registry.ok" {
		t.Fatalf("Active() changed after input mutation: %+v", stillActive.Diagnostics)
	}
	if got := stillActive.Capabilities["cap.beta"]; got.Title != "Beta" {
		t.Fatalf("Active() capability changed after input mutation: %+v", got)
	}
}

func TestPublishKeepsPreviousSnapshotOnFailure(t *testing.T) {
	events := make([]capturedEvent, 0, 1)
	service := newReloadTestService(t, "digest-a", func(typ runtimeevents.Type, payload any) {
		raw, err := runtimeevents.EncodePayload(payload)
		if err != nil {
			t.Fatalf("EncodePayload() error = %v", err)
		}
		events = append(events, capturedEvent{typ: typ, payload: raw})
	})

	before := service.Active()
	err := service.Publish(Snapshot{Digest: "digest-b"})
	if err == nil {
		t.Fatal("Publish() error = nil, want error")
	}

	after := service.Active()
	if after.Digest != before.Digest {
		t.Fatalf("Active().Digest = %q, want %q", after.Digest, before.Digest)
	}
	if after.Capabilities["cap.alpha"].Title != before.Capabilities["cap.alpha"].Title {
		t.Fatalf("Active().Capabilities changed on rejected publish: %+v", after.Capabilities)
	}
	if len(events) != 1 {
		t.Fatalf("events len = %d, want 1", len(events))
	}
	if events[0].typ != runtimeevents.EventCapabilitySnapshotRejected {
		t.Fatalf("event type = %q, want %q", events[0].typ, runtimeevents.EventCapabilitySnapshotRejected)
	}

	decoded, err := runtimeevents.DecodePayload[runtimeevents.CapabilitySnapshotRejectedPayload](events[0].payload)
	if err != nil {
		t.Fatalf("DecodePayload(CapabilitySnapshotRejectedPayload) error = %v", err)
	}
	if decoded.Digest != "digest-b" {
		t.Fatalf("rejected payload digest = %q, want digest-b", decoded.Digest)
	}
	if decoded.PreviousDigest != before.Digest {
		t.Fatalf("rejected payload previous digest = %q, want %q", decoded.PreviousDigest, before.Digest)
	}
	if decoded.Reason == "" {
		t.Fatal("rejected payload reason = empty, want validation error")
	}
}

func TestPublishEmitsCapabilitySnapshotEvents(t *testing.T) {
	events := make([]capturedEvent, 0, 2)
	service := newReloadTestService(t, "digest-a", func(typ runtimeevents.Type, payload any) {
		raw, err := runtimeevents.EncodePayload(payload)
		if err != nil {
			t.Fatalf("EncodePayload() error = %v", err)
		}
		events = append(events, capturedEvent{typ: typ, payload: raw})
	})

	next := testSnapshot("digest-b", "beta")
	if err := service.Publish(next); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if err := service.Publish(Snapshot{Digest: "digest-c"}); err == nil {
		t.Fatal("Publish() error = nil, want error")
	}

	if len(events) != 2 {
		t.Fatalf("events len = %d, want 2", len(events))
	}
	if events[0].typ != runtimeevents.EventCapabilitySnapshotPublished {
		t.Fatalf("event[0] type = %q, want %q", events[0].typ, runtimeevents.EventCapabilitySnapshotPublished)
	}
	if events[1].typ != runtimeevents.EventCapabilitySnapshotRejected {
		t.Fatalf("event[1] type = %q, want %q", events[1].typ, runtimeevents.EventCapabilitySnapshotRejected)
	}

	published, err := runtimeevents.DecodePayload[runtimeevents.CapabilitySnapshotPublishedPayload](events[0].payload)
	if err != nil {
		t.Fatalf("DecodePayload(CapabilitySnapshotPublishedPayload) error = %v", err)
	}
	if published.Digest != next.Digest {
		t.Fatalf("published payload digest = %q, want %q", published.Digest, next.Digest)
	}
	if published.PreviousDigest != "digest-a" {
		t.Fatalf("published payload previous digest = %q, want digest-a", published.PreviousDigest)
	}
	if published.CapabilityCount != len(next.Capabilities) {
		t.Fatalf("published payload capability count = %d, want %d", published.CapabilityCount, len(next.Capabilities))
	}

	rejected, err := runtimeevents.DecodePayload[runtimeevents.CapabilitySnapshotRejectedPayload](events[1].payload)
	if err != nil {
		t.Fatalf("DecodePayload(CapabilitySnapshotRejectedPayload) error = %v", err)
	}
	if rejected.Digest != "digest-c" {
		t.Fatalf("rejected payload digest = %q, want digest-c", rejected.Digest)
	}
	if rejected.PreviousDigest != "digest-b" {
		t.Fatalf("rejected payload previous digest = %q, want digest-b", rejected.PreviousDigest)
	}
}

func TestReloadDoesNotOverwriteNewerPublish(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	publishedApplied := make(chan struct{})
	allowReloadReturn := make(chan struct{})
	events := make([]capturedEvent, 0, 2)
	var eventsMu sync.Mutex
	var startedOnce sync.Once
	var publishedOnce sync.Once
	service, err := NewService(testSnapshot("digest-a", "alpha"),
		WithLoader(func(ctx context.Context) (Snapshot, error) {
			startedOnce.Do(func() { close(started) })
			<-release
			return testSnapshot("digest-stale", "beta"), nil
		}),
		WithEventSink(func(typ runtimeevents.Type, payload any) {
			raw, err := runtimeevents.EncodePayload(payload)
			if err != nil {
				t.Fatalf("EncodePayload() error = %v", err)
			}
			if typ == runtimeevents.EventCapabilitySnapshotPublished {
				decoded, err := runtimeevents.DecodePayload[runtimeevents.CapabilitySnapshotPublishedPayload](raw)
				if err != nil {
					t.Fatalf("DecodePayload(CapabilitySnapshotPublishedPayload) error = %v", err)
				}
				if decoded.Digest == "digest-stale" {
					publishedOnce.Do(func() { close(publishedApplied) })
					<-allowReloadReturn
				}
			}
			eventsMu.Lock()
			defer eventsMu.Unlock()
			events = append(events, capturedEvent{typ: typ, payload: raw})
		}),
	)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	type reloadResult struct {
		snapshot Snapshot
		err      error
	}
	reloadResultCh := make(chan reloadResult, 1)
	go func() {
		snapshot, err := service.Reload(context.Background())
		reloadResultCh <- reloadResult{snapshot: snapshot, err: err}
	}()

	<-started
	close(release)
	<-publishedApplied
	if err := service.Publish(testSnapshot("digest-b", "beta")); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	close(allowReloadReturn)

	result := <-reloadResultCh
	if result.err != nil {
		t.Fatalf("Reload() error = %v, want nil", result.err)
	}
	if result.snapshot.Digest != "digest-stale" {
		t.Fatalf("Reload() snapshot digest = %q, want digest-stale", result.snapshot.Digest)
	}

	active := service.Active()
	if active.Digest != "digest-b" {
		t.Fatalf("Active().Digest = %q, want digest-b", active.Digest)
	}

	eventsMu.Lock()
	defer eventsMu.Unlock()
	if len(events) != 2 {
		t.Fatalf("events len = %d, want 2", len(events))
	}
	if events[0].typ != runtimeevents.EventCapabilitySnapshotPublished {
		t.Fatalf("event[0] type = %q, want %q", events[0].typ, runtimeevents.EventCapabilitySnapshotPublished)
	}
	if events[1].typ != runtimeevents.EventCapabilitySnapshotPublished {
		t.Fatalf("event[1] type = %q, want %q", events[1].typ, runtimeevents.EventCapabilitySnapshotPublished)
	}
	digests := map[string]bool{}
	for i, event := range events {
		published, err := runtimeevents.DecodePayload[runtimeevents.CapabilitySnapshotPublishedPayload](event.payload)
		if err != nil {
			t.Fatalf("DecodePayload(CapabilitySnapshotPublishedPayload) event[%d] error = %v", i, err)
		}
		digests[published.Digest] = true
	}
	if !digests["digest-stale"] || !digests["digest-b"] {
		t.Fatalf("published digests = %+v, want digest-stale and digest-b", digests)
	}
}

func newReloadTestService(t *testing.T, digest string, sink EventSink) *Service {
	t.Helper()

	service, err := NewService(testSnapshot(digest, "alpha"), WithEventSink(sink))
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	return service
}

func testSnapshot(digest string, suffix string) Snapshot {
	key := "cap." + suffix
	return Snapshot{
		Digest: digest,
		Diagnostics: []registry.Diagnostic{{
			Code:    "registry.ok",
			Message: "ok",
		}},
		Capabilities: map[string]Descriptor{
			key: {
				Kind:  registry.KindSkill,
				Key:   key,
				Title: titleFromSuffix(suffix),
				Tags:  []string{"alpha"},
			},
		},
	}
}

func titleFromSuffix(suffix string) string {
	switch suffix {
	case "alpha":
		return "Alpha"
	case "beta":
		return "Beta"
	default:
		return suffix
	}
}
