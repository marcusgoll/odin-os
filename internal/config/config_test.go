package config

import "testing"

func TestDefaultConfigIsDryRun(t *testing.T) {
	t.Parallel()

	cfg := Default()
	if !cfg.DryRun {
		t.Fatal("Default().DryRun = false, want true")
	}
	if cfg.KillSwitch {
		t.Fatal("Default().KillSwitch = true, want false")
	}
	if cfg.WorkspaceRoot == "" {
		t.Fatal("Default().WorkspaceRoot is empty")
	}
	if cfg.LogDir == "" {
		t.Fatal("Default().LogDir is empty")
	}
}

func TestDefaultConfigUsesLocalRuntimePaths(t *testing.T) {
	t.Parallel()

	cfg := Default()
	if cfg.WorkspaceRoot != "workspaces" {
		t.Fatalf("Default().WorkspaceRoot = %q, want %q", cfg.WorkspaceRoot, "workspaces")
	}
	if cfg.LogDir != "logs" {
		t.Fatalf("Default().LogDir = %q, want %q", cfg.LogDir, "logs")
	}
}
