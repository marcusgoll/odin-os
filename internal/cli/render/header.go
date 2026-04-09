package render

import "fmt"

type Header struct {
	Scope            string
	Mode             string
	Health           string
	PendingApprovals int
	ActiveTask       string
	ActiveRun        string
}

func RenderHeader(header Header) string {
	rendered := fmt.Sprintf(
		"scope=%s mode=%s health=%s approvals=%d",
		header.Scope,
		header.Mode,
		header.Health,
		header.PendingApprovals,
	)

	if header.ActiveTask != "" {
		rendered += " task=" + header.ActiveTask
	}
	if header.ActiveRun != "" {
		rendered += " run=" + header.ActiveRun
	}

	return rendered
}
