package proposals

import (
	"context"
	"path/filepath"
	"testing"

	"odin-os/internal/store/sqlite"
)

func TestProposalServiceCreatesSubmitsAndRejects(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openProposalStore(t)
	defer store.Close()

	service := Service{Store: store}

	proposal, err := service.Create(ctx, CreateInput{
		ProposalType:      "prompt_refinement",
		Scope:             "global",
		TargetKey:         "workers/planner",
		Summary:           "Shorten planner instruction preamble",
		Hypothesis:        "Lower token use without losing task quality",
		ChangePayloadJSON: `{"trim_intro":true}`,
		CreatedBy:         "odin",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if proposal.Status != "draft" {
		t.Fatalf("proposal.Status = %q, want %q", proposal.Status, "draft")
	}

	proposal, err = service.Submit(ctx, proposal.ID)
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if proposal.Status != "submitted" {
		t.Fatalf("submitted proposal.Status = %q, want %q", proposal.Status, "submitted")
	}

	proposal, err = service.Reject(ctx, proposal.ID)
	if err != nil {
		t.Fatalf("Reject() error = %v", err)
	}
	if proposal.Status != "rejected" {
		t.Fatalf("rejected proposal.Status = %q, want %q", proposal.Status, "rejected")
	}
}

func openProposalStore(t *testing.T) *sqlite.Store {
	t.Helper()

	store, err := sqlite.Open(filepath.Join(t.TempDir(), "odin.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	return store
}
