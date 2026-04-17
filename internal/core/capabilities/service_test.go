package capabilities

import (
	"testing"

	"odin-os/internal/registry"
)

func TestServiceExposesActiveSnapshot(t *testing.T) {
	item := registry.Item{
		Kind:   registry.KindSkill,
		Key:    "skill.example",
		Title:  "Example Skill",
		Tags:   []string{"alpha"},
		Scopes: []string{"project"},
		Sections: map[string]string{
			"Purpose": "Example",
		},
	}

	snapshot := Snapshot{
		Digest:      "digest-123",
		Diagnostics: []registry.Diagnostic{{Code: "registry.ok", Message: "ok"}},
		Capabilities: map[string]Descriptor{
			item.Key: item,
		},
	}

	service, err := NewService(snapshot)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	active := service.Active()
	if active.Digest != snapshot.Digest {
		t.Fatalf("Active().Digest = %q, want %q", active.Digest, snapshot.Digest)
	}
	if len(active.Diagnostics) != 1 || active.Diagnostics[0].Code != "registry.ok" {
		t.Fatalf("Active().Diagnostics = %+v, want one registry.ok diagnostic", active.Diagnostics)
	}
	if got := active.Capabilities[item.Key]; got.Key != item.Key || got.Title != item.Title {
		t.Fatalf("Active().Capabilities[%q] = %+v, want %+v", item.Key, got, item)
	}

	snapshot.Diagnostics[0].Code = "mutated"
	mutatedDescriptor := snapshot.Capabilities[item.Key]
	mutatedDescriptor.Tags = []string{"changed"}
	snapshot.Capabilities[item.Key] = mutatedDescriptor

	activeAfterMutation := service.Active()
	if activeAfterMutation.Diagnostics[0].Code != "registry.ok" {
		t.Fatalf("Active() changed after input mutation: %+v", activeAfterMutation.Diagnostics)
	}
	if got := activeAfterMutation.Capabilities[item.Key]; len(got.Tags) != 1 || got.Tags[0] != "alpha" {
		t.Fatalf("Active() capabilities changed after input mutation: %+v", got)
	}
}

func TestServiceRejectsNilSnapshot(t *testing.T) {
	_, err := NewService(Snapshot{})
	if err == nil {
		t.Fatal("NewService() error = nil, want error")
	}
}
