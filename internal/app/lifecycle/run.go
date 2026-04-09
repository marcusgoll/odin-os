package lifecycle

import (
	"context"
	"io"

	"odin-os/internal/app/bootstrap"
	"odin-os/internal/cli/repl"
)

// Run boots the Odin interactive shell for the provided repository root.
func Run(ctx context.Context, root string, stdin io.Reader, stdout io.Writer) error {
	app, err := bootstrap.Load(ctx, root)
	if err != nil {
		return err
	}
	defer app.Store.Close()

	shell, err := repl.New(repl.Environment{
		Store:               app.Store,
		Registry:            app.Registry,
		RegistryDiagnostics: app.RegistryDiagnostics,
		SessionStore:        app.SessionStore,
	})
	if err != nil {
		return err
	}

	if err := shell.Run(ctx, stdin, stdout); err != nil && err != io.EOF {
		return err
	}
	return nil
}
