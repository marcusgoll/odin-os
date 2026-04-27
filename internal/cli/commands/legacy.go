package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	legacyobs "odin-os/internal/runtime/legacy"
)

func RunLegacy(ctx context.Context, args []string, stdout io.Writer) error {
	return RunLegacyWithService(ctx, legacyobs.DefaultService(), args, stdout)
}

func RunLegacyWithService(ctx context.Context, service legacyobs.Service, args []string, stdout io.Writer) error {
	jsonOutput := false
	if len(args) == 0 {
		args = []string{"status"}
	}
	if len(args) > 0 && args[0] == "--json" {
		jsonOutput = true
		args = append([]string{"status"}, args[1:]...)
	}
	if len(args) > 1 && args[1] == "--json" {
		jsonOutput = true
		args = append([]string{args[0]}, args[2:]...)
	}
	if len(args) != 1 {
		_, err := fmt.Fprintf(stdout, "usage: %s\n", LegacyUsage)
		return err
	}

	switch {
	case strings.EqualFold(args[0], "status"):
		report, err := service.Report(ctx)
		if err != nil {
			return err
		}
		if jsonOutput {
			return writeLegacyJSON(stdout, report)
		}
		_, err = fmt.Fprint(stdout, legacyobs.RenderText(report))
		return err
	case strings.EqualFold(args[0], "capabilities"):
		report, err := service.Capabilities(ctx)
		if err != nil {
			return err
		}
		if jsonOutput {
			return writeLegacyJSON(stdout, report)
		}
		_, err = fmt.Fprint(stdout, legacyobs.RenderCapabilityText(report))
		return err
	default:
		_, err := fmt.Fprintf(stdout, "usage: %s\n", LegacyUsage)
		return err
	}
}

func writeLegacyJSON(stdout io.Writer, value any) error {
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}
