package main

import (
	"context"
	"fmt"
	"os"

	"odin-os/internal/app/lifecycle"
)

func main() {
	if err := lifecycle.Run(context.Background(), os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
