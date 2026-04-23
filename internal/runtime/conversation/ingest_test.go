package conversation

import (
	"context"
	"strings"
	"testing"

	"odin-os/internal/cli/scope"
	"odin-os/internal/store/sqlite"
)

func TestIngestAskStoresTranscript(t *testing.T) {
	t.Parallel()

	service, store := newTestService(t)

	result, err := service.Respond(context.Background(), Request{
		Scope:  scope.Resolution{Kind: scope.ScopeGlobal},
		Mode:   "ask",
		Prompt: "hello there",
	})
	if err != nil {
		t.Fatalf("Respond() error = %v", err)
	}
	if strings.TrimSpace(result.Answer) == "" {
		t.Fatalf("Answer is empty")
	}

	transcripts, err := store.ListConversationTranscripts(context.Background(), sqlite.ListConversationTranscriptsParams{
		Scope:    "global",
		ScopeKey: "global",
		Mode:     "ask",
	})
	if err != nil {
		t.Fatalf("ListConversationTranscripts() error = %v", err)
	}
	if len(transcripts) != 1 {
		t.Fatalf("transcripts len = %d, want 1", len(transcripts))
	}
	if transcripts[0].Prompt != "hello there" {
		t.Fatalf("Prompt = %q, want %q", transcripts[0].Prompt, "hello there")
	}
	if strings.TrimSpace(transcripts[0].Response) == "" {
		t.Fatalf("Response is empty")
	}
}
