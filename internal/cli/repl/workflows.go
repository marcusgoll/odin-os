package repl

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"odin-os/internal/registry"
)

func (shell *Shell) handleWorkflows(args []string, output io.Writer) error {
	workflows := append([]registry.Item(nil), shell.env.RegistrySnapshot.ByKind[registry.KindWorkflow]...)
	if len(workflows) == 0 {
		_, err := fmt.Fprintln(output, "no workflows loaded")
		return err
	}

	sort.Slice(workflows, func(i int, j int) bool {
		return workflows[i].Key < workflows[j].Key
	})

	if len(args) == 0 || strings.EqualFold(args[0], "list") {
		for _, workflow := range workflows {
			status := workflow.Status
			if status == "" {
				status = "unknown"
			}
			if _, err := fmt.Fprintf(output, "%s status=%s entrypoint=%s summary=%s\n", workflow.Key, status, workflow.Entrypoint, workflow.Summary); err != nil {
				return err
			}
		}
		return nil
	}

	key := strings.TrimSpace(args[0])
	for _, workflow := range workflows {
		if workflow.Key != key {
			continue
		}
		status := workflow.Status
		if status == "" {
			status = "unknown"
		}
		if _, err := fmt.Fprintf(output, "key=%s\n", workflow.Key); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(output, "title=%s\n", workflow.Title); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(output, "status=%s\n", status); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(output, "entrypoint=%s\n", workflow.Entrypoint); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(output, "composes=%s\n", strings.Join(workflow.Composes, ",")); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(output, "source=%s\n", workflow.Source.RelativePath); err != nil {
			return err
		}
		purpose := strings.TrimSpace(workflow.Sections[registry.SectionPurpose])
		if purpose != "" {
			if _, err := fmt.Fprintf(output, "purpose=%s\n", compactLine(purpose)); err != nil {
				return err
			}
		}
		return nil
	}

	_, err := fmt.Fprintf(output, "unknown workflow: %s\n", key)
	return err
}

func compactLine(value string) string {
	return strings.Join(strings.Fields(value), " ")
}
