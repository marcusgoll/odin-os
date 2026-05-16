package browserprofilearchive

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPackUnpackRoundTripsBrowserProfileDirectory(t *testing.T) {
	source := t.TempDir()
	if err := os.MkdirAll(filepath.Join(source, "Default", "Local Storage"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "Default", "Preferences"), []byte(`{"profile":"ok"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "Default", "Local Storage", "leveldb"), []byte("state"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "SingletonLock"), []byte("lock"), 0o600); err != nil {
		t.Fatal(err)
	}

	payload, err := Pack(source)
	if err != nil {
		t.Fatalf("Pack() error = %v", err)
	}
	target := filepath.Join(t.TempDir(), "profile")
	if err := Unpack(payload, target); err != nil {
		t.Fatalf("Unpack() error = %v", err)
	}
	got, err := os.ReadFile(filepath.Join(target, "Default", "Preferences"))
	if err != nil {
		t.Fatalf("ReadFile(Preferences) error = %v", err)
	}
	if string(got) != `{"profile":"ok"}` {
		t.Fatalf("Preferences = %q", got)
	}
	if _, err := os.Stat(filepath.Join(target, "SingletonLock")); !os.IsNotExist(err) {
		t.Fatalf("SingletonLock stat err=%v, want skipped volatile lock", err)
	}
}

func TestUnpackRejectsEscapingArchiveEntries(t *testing.T) {
	target := filepath.Join(t.TempDir(), "profile")
	if err := Unpack([]byte("not-gzip"), target); err == nil {
		t.Fatalf("Unpack(invalid) error = nil, want gzip rejection")
	}
	if _, err := normalizeArchiveName("../escape"); err == nil {
		t.Fatalf("normalizeArchiveName escape error = nil")
	}
}
