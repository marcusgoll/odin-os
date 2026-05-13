package github

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"odin-os/internal/tracker"
)

func TestLiveGitHubTrackerSmoke(t *testing.T) {
	if os.Getenv("ODIN_LIVE_GITHUB_TRACKER_SMOKE") != "1" {
		t.Skip("set ODIN_LIVE_GITHUB_TRACKER_SMOKE=1 to run the live GitHub tracker smoke test")
	}

	repoID := strings.TrimSpace(os.Getenv("ODIN_LIVE_GITHUB_REPO"))
	owner, repo, ok := strings.Cut(repoID, "/")
	if !ok || strings.TrimSpace(owner) == "" || strings.TrimSpace(repo) == "" {
		t.Fatal("ODIN_LIVE_GITHUB_REPO must be owner/repo for a disposable repository")
	}
	issueNumber, err := strconv.Atoi(strings.TrimSpace(os.Getenv("ODIN_LIVE_GITHUB_ISSUE")))
	if err != nil || issueNumber <= 0 {
		t.Fatal("ODIN_LIVE_GITHUB_ISSUE must be a positive disposable issue number")
	}
	if strings.TrimSpace(os.Getenv("GITHUB_TOKEN")) == "" {
		t.Fatal("GITHUB_TOKEN must be set with issue read/write scope for the disposable repository")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client := NewClientWithConfig(Config{
		Owner:    owner,
		Repo:     repo,
		TokenEnv: "GITHUB_TOKEN",
	})
	issueID := tracker.IssueID{
		Provider: "github",
		Repo:     repoID,
		Number:   issueNumber,
	}

	issue, err := client.FetchIssueByID(ctx, issueID)
	if err != nil {
		t.Fatalf("FetchIssueByID() error = %v", err)
	}
	if issue.Number != issueNumber || issue.Repo != repoID || issue.Provider != "github" {
		t.Fatalf("FetchIssueByID() issue = %+v, want github %s #%d", issue, repoID, issueNumber)
	}

	if err := client.MarkInProgress(ctx, issueID); err != nil {
		t.Fatalf("MarkInProgress() error = %v", err)
	}
	comment := fmt.Sprintf("odin live tracker smoke %s", time.Now().UTC().Format(time.RFC3339))
	if err := client.AddComment(ctx, issueID, comment); err != nil {
		t.Fatalf("AddComment() error = %v", err)
	}

	dryRunClient := NewClientWithConfig(Config{
		BaseURL:  "http://127.0.0.1:1",
		Owner:    owner,
		Repo:     repo,
		TokenEnv: "GITHUB_TOKEN",
		DryRun:   true,
	})
	if err := dryRunClient.MarkBlocked(ctx, issueID, "dry-run blocked smoke"); err != nil {
		t.Fatalf("dry-run MarkBlocked() error = %v", err)
	}
	if err := dryRunClient.AddComment(ctx, issueID, "dry-run comment smoke"); err != nil {
		t.Fatalf("dry-run AddComment() error = %v", err)
	}
	if _, err := dryRunClient.CreateFollowUpIssue(ctx, tracker.FollowUpIssue{
		Repo:   repoID,
		Title:  "dry-run follow-up smoke",
		Body:   "dry-run only",
		Labels: []string{tracker.LabelReady},
	}); err != nil {
		t.Fatalf("dry-run CreateFollowUpIssue() error = %v", err)
	}
}
