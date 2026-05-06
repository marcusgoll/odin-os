package lifecycle

import (
	"context"
	"fmt"
	"io"
	"strings"

	"odin-os/internal/app/bootstrap"
	commands "odin-os/internal/cli/commands"
	browserexecutor "odin-os/internal/executors/browser"
)

type browserRunView struct {
	Status               string                       `json:"status"`
	GoalID               int64                        `json:"goal_id"`
	EvidenceID           int64                        `json:"evidence_id"`
	EvidenceType         string                       `json:"evidence_type"`
	AdapterStatus        string                       `json:"adapter_status,omitempty"`
	AdapterKind          string                       `json:"adapter_kind,omitempty"`
	StartURLs            []string                     `json:"start_urls"`
	AllowedDomains       []string                     `json:"allowed_domains"`
	MaxPages             int                          `json:"max_pages"`
	MaxDurationSeconds   int                          `json:"max_duration_seconds"`
	VisitedURLs          []string                     `json:"visited_urls,omitempty"`
	PageResults          []browserexecutor.PageResult `json:"page_results,omitempty"`
	ExtractedTextSummary string                       `json:"extracted_text_summary,omitempty"`
	Screenshots          []string                     `json:"screenshots,omitempty"`
	ActionLog            []string                     `json:"action_log,omitempty"`
}

func runBrowser(ctx context.Context, app bootstrap.App, args []string, stdout io.Writer) error {
	command, err := commands.ParseBrowser(args)
	if err != nil {
		return err
	}
	if command.Name == "help" {
		_, err := fmt.Fprintln(stdout, commands.BrowserUsage)
		return err
	}

	goal, err := app.Store.GetGoal(ctx, command.GoalID)
	if err != nil {
		return err
	}
	objective := strings.TrimSpace(command.Objective)
	if objective == "" {
		objective = goal.Title
	}
	result, err := browserexecutor.Service{Store: app.Store}.Run(ctx, browserexecutor.ReadOnlyTask{
		GoalID:             command.GoalID,
		WorkerMode:         command.WorkerMode,
		Objective:          objective,
		AllowedDomains:     command.AllowedDomains,
		StartURLs:          command.URLs,
		MaxPages:           command.MaxPages,
		MaxDurationSeconds: command.MaxDurationSeconds,
		EvidenceRequired:   command.EvidenceRequired,
		Actions:            command.Actions,
	})
	if err != nil {
		return err
	}
	view := browserRunView{
		Status:               result.Status,
		GoalID:               result.GoalID,
		EvidenceID:           result.EvidenceID,
		EvidenceType:         result.EvidenceType,
		AdapterStatus:        result.AdapterStatus,
		AdapterKind:          result.AdapterKind,
		StartURLs:            result.StartURLs,
		AllowedDomains:       result.AllowedDomains,
		MaxPages:             result.MaxPages,
		MaxDurationSeconds:   result.MaxDurationSeconds,
		VisitedURLs:          result.VisitedURLs,
		PageResults:          result.PageResults,
		ExtractedTextSummary: result.ExtractedTextSummary,
		Screenshots:          result.Screenshots,
		ActionLog:            result.ActionLog,
	}
	if command.JSON {
		return commands.WriteJSON(stdout, view)
	}
	_, err = fmt.Fprintf(stdout, "browser goal=%d status=%s evidence=%d type=%s\n", view.GoalID, view.Status, view.EvidenceID, view.EvidenceType)
	return err
}
