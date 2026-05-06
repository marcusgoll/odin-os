package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	worker "odin-os/internal/workers/huginnbrowser"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := (worker.Worker{}).Run(ctx, os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
