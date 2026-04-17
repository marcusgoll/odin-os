package commands

import (
	"context"
	"fmt"
	"io"

	coreprofile "odin-os/internal/core/profile"
	"odin-os/internal/store/sqlite"
)

type ProfileCommand struct {
	Action     string
	QuietHours string
}

func ParseProfile(args []string) (ProfileCommand, error) {
	if len(args) == 0 || args[0] == "show" {
		return ProfileCommand{Action: "show"}, nil
	}
	if args[0] != "set" {
		return ProfileCommand{}, fmt.Errorf("unknown profile command: %s", args[0])
	}

	cmd := ProfileCommand{Action: "set"}
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--quiet-hours":
			if i+1 >= len(args) {
				return ProfileCommand{}, fmt.Errorf("--quiet-hours requires a value")
			}
			cmd.QuietHours = args[i+1]
			i++
		default:
			return ProfileCommand{}, fmt.Errorf("unknown profile flag: %s", args[i])
		}
	}
	if cmd.QuietHours == "" {
		return ProfileCommand{}, fmt.Errorf("profile set requires at least one update")
	}
	return cmd, nil
}

func RunProfile(ctx context.Context, store *sqlite.Store, args []string, stdout io.Writer) error {
	if store == nil {
		return fmt.Errorf("profile store is required")
	}

	command, err := ParseProfile(args)
	if err != nil {
		return err
	}

	service := coreprofile.Service{
		Store:       store,
		WorkspaceID: coreprofile.DefaultWorkspaceID,
	}

	switch command.Action {
	case "show":
		profile, err := service.Get(ctx)
		if err != nil {
			return err
		}
		return renderProfile(stdout, profile)
	case "set":
		profile, err := service.Update(ctx, coreprofile.UpdateParams{
			QuietHours: ptr(command.QuietHours),
		})
		if err != nil {
			return err
		}
		return renderProfile(stdout, profile)
	default:
		return fmt.Errorf("unknown profile action: %s", command.Action)
	}
}

func renderProfile(stdout io.Writer, profile coreprofile.OperatingProfile) error {
	quietHours := profile.Preferences.QuietHours
	if quietHours == "" {
		quietHours = "unset"
	}

	_, err := fmt.Fprintf(stdout, "workspace=%s quiet_hours=%s approval_required=%t\n",
		profile.WorkspaceID,
		quietHours,
		profile.Boundaries.ApprovalDefaults.RequireHumanApprovalForExternalEffects,
	)
	return err
}

func ptr[T any](value T) *T {
	return &value
}
