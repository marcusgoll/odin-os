package render

import "fmt"

type Header struct {
	Scope            string
	Mode             string
	Health           string
	PendingApprovals int
	SelectedSkill    string
	SelectedWorkflow string
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
	if header.SelectedSkill != "" {
		rendered += " skill=" + header.SelectedSkill
	}
	if header.SelectedWorkflow != "" {
		rendered += " workflow=" + header.SelectedWorkflow
	}

	return rendered
}
