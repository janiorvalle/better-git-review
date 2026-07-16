package patch

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/janiorvalle/better-git-review/internal/document"
	"github.com/janiorvalle/better-git-review/internal/source"
)

type Source struct{}

func (Source) Name() string {
	return "patch"
}

func (Source) Detect(opts source.Options) (bool, string) {
	if opts.DiffFile == "" {
		return false, "no patch file requested"
	}
	return true, "patch file requested"
}

func (Source) Collect(_ context.Context, opts source.Options) (source.Result, error) {
	var (
		diffBytes []byte
		err       error
		name      string
		title     string
		rangeText string
	)
	if opts.DiffFile == "-" {
		opts.Logf("reading diff from stdin ...")
		if opts.Stdin == nil {
			opts.Stdin = os.Stdin
		}
		diffBytes, err = io.ReadAll(opts.Stdin)
		name, title, rangeText = "stdin", "stdin patch", "stdin"
	} else {
		opts.Logf("reading diff from %s ...", opts.DiffFile)
		diffBytes, err = os.ReadFile(opts.DiffFile)
		base := filepath.Base(opts.DiffFile)
		name = strings.TrimSuffix(base, filepath.Ext(base))
		title, rangeText = base, opts.DiffFile
	}
	if err != nil {
		return source.Result{}, fmt.Errorf("read diff %q: %w", opts.DiffFile, err)
	}
	return source.Result{
		Diff: diffBytes,
		Source: document.Source{
			Title:   title,
			Range:   rangeText,
			Name:    source.SafeName(name),
			RepoDir: "",
		},
	}, nil
}
