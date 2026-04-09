package main

import (
	"context"
	"fmt"
	"os"

	"odin-os/internal/app/lifecycle"
)

func main() {
	root, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if err := lifecycle.Run(context.Background(), root, os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
