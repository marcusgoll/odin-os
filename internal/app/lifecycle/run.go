package lifecycle

import (
	"context"
	"fmt"
	"io"
)

const scaffoldMessage = "Odin OS scaffold initialized. Implementation phases pending."

// Run emits the minimal process message for the Phase 01 scaffold.
func Run(ctx context.Context, stdout io.Writer) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	_, err := fmt.Fprintln(stdout, scaffoldMessage)
	return err
}
