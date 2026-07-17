package main

import (
	"context"
	"errors"
	"flag"
	"os"

	"github.com/janiorvalle/better-git-review/internal/app"
	"github.com/janiorvalle/better-git-review/internal/terminal"
)

func main() {
	if err := app.Run(context.Background(), os.Args[1:], app.Environment{}); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		info, _ := os.Stderr.Stat()
		styled := info != nil && info.Mode()&os.ModeCharDevice != 0 && os.Getenv("NO_COLOR") == ""
		terminal.Error(os.Stderr, err.Error(), styled)
		os.Exit(1)
	}
}
