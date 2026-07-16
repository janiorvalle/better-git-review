package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/janiorvalle/better-git-review/internal/app"
)

func main() {
	if err := app.Run(context.Background(), os.Args[1:], app.Environment{}); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		fmt.Fprintf(os.Stderr, "bgr: %v\n", err)
		os.Exit(1)
	}
}
