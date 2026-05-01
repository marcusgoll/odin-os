package tui

import (
	"context"
	"fmt"
	"io"

	"odin-os/internal/registry"
	"odin-os/internal/runtime/projections"
	"odin-os/internal/store/sqlite"
)

const usage = "usage: odin tui --once"

func Run(ctx context.Context, store *sqlite.Store, snapshot registry.Snapshot, args []string, stdout io.Writer) error {
	if len(args) != 1 || args[0] != "--once" {
		_, err := fmt.Fprintln(stdout, usage)
		return err
	}
	return RenderOnce(ctx, store, snapshot, stdout)
}

func RenderOnce(ctx context.Context, store *sqlite.Store, snapshot registry.Snapshot, stdout io.Writer) error {
	taskViews, err := projections.ListTaskStatusViews(ctx, store.DB())
	if err != nil {
		return err
	}
	runViews, err := projections.ListRunSummaryViews(ctx, store.DB())
	if err != nil {
		return err
	}
	approvalViews, err := projections.ListPendingApprovalViews(ctx, store.DB())
	if err != nil {
		return err
	}

	openWorkItems := 0
	for _, view := range taskViews {
		if isOpenWorkItemStatus(view.Status) {
			openWorkItems++
		}
	}
	activeRunAttempts := 0
	for _, view := range runViews {
		if isActiveRunAttemptStatus(view.Status) {
			activeRunAttempts++
		}
	}

	if _, err := fmt.Fprintln(stdout, "Odin TUI"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(stdout, "Operator Surface: odin work ..."); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(stdout, "Work Items: total=%d open=%d\n", len(taskViews), openWorkItems); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(stdout, "Run Attempts: total=%d active=%d\n", len(runViews), activeRunAttempts); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(stdout, "Approvals: pending=%d\n", len(approvalViews)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(stdout, "Delivery Profiles: total=%d\n", len(deliveryProfiles(snapshot))); err != nil {
		return err
	}
	return nil
}

func isOpenWorkItemStatus(status string) bool {
	switch status {
	case "queued", "running", "blocked":
		return true
	default:
		return false
	}
}

func isActiveRunAttemptStatus(status string) bool {
	switch status {
	case "queued", "running":
		return true
	default:
		return false
	}
}

func deliveryProfiles(snapshot registry.Snapshot) []registry.Item {
	var profiles []registry.Item
	for _, item := range snapshot.ByKind[registry.KindWorkflow] {
		for _, tag := range item.Tags {
			if tag == "delivery_profile" {
				profiles = append(profiles, item)
				break
			}
		}
	}
	return profiles
}
