package commands

import (
	"fmt"
	"io"
	"time"

	"odin-os/internal/runtime/projections"
)

type AgendaCommand struct {
	JSON bool
}

func ParseAgenda(args []string) (AgendaCommand, error) {
	command := AgendaCommand{}
	for _, arg := range args {
		switch arg {
		case "--json":
			if command.JSON {
				return AgendaCommand{}, fmt.Errorf("duplicate --json flag")
			}
			command.JSON = true
		default:
			return AgendaCommand{}, fmt.Errorf("usage: odin agenda [--json]")
		}
	}
	return command, nil
}

func WriteAgendaText(w io.Writer, view projections.AgendaView) error {
	if len(view.DueWork) == 0 && len(view.BlockedWork) == 0 && len(view.Approvals) == 0 {
		_, err := fmt.Fprintln(w, "no agenda items")
		return err
	}

	for _, followUp := range view.DueWork {
		initiativeKey := "none"
		if followUp.InitiativeKey != nil {
			initiativeKey = *followUp.InitiativeKey
		}
		if _, err := fmt.Fprintf(
			w,
			"due %s %s %s next_due_at=%s\n",
			followUp.DueStatus,
			initiativeKey,
			followUp.Title,
			followUp.NextDueAt.UTC().Format(time.RFC3339),
		); err != nil {
			return err
		}
	}

	for _, blocked := range view.BlockedWork {
		if _, err := fmt.Fprintf(w, "blocked %s %s reason=%s\n", blocked.TaskKey, blocked.Source, blocked.Reason); err != nil {
			return err
		}
	}

	for _, approval := range view.Approvals {
		if _, err := fmt.Fprintf(w, "approval %s %s\n", approval.TaskKey, approval.Status); err != nil {
			return err
		}
	}

	return nil
}
