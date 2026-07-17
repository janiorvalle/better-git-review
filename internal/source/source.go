package source

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/janiorvalle/better-git-review/internal/document"
	"github.com/janiorvalle/better-git-review/internal/gitexec"
)

type Options struct {
	PR       string
	DiffFile string
	Base     string
	Head     string
	Commit   string
	Dirty    bool
	RepoDir  string
	Stdin    io.Reader
	Logf     func(string, ...any)
}

type Result struct {
	Source  document.Source
	Diff    []byte
	BaseRef string
	HeadRef string
	Dirty   bool
}

type Source interface {
	Name() string
	Detect(Options) (available bool, detail string)
	Collect(context.Context, Options) (Result, error)
}

type Registry struct {
	sources []Source
}

func NewRegistry(sources ...Source) Registry {
	return Registry{sources: append([]Source(nil), sources...)}
}

func (r Registry) Collect(ctx context.Context, opts Options) (Result, error) {
	if opts.Logf == nil {
		opts.Logf = func(string, ...any) {}
	}
	repoDir, err := filepath.Abs(opts.RepoDir)
	if err != nil {
		return Result{}, err
	}
	opts.RepoDir = repoDir

	var probes []string
	for _, candidate := range r.sources {
		available, detail := candidate.Detect(opts)
		probes = append(probes, fmt.Sprintf("%s: %s", candidate.Name(), detail))
		if !available {
			continue
		}
		result, err := candidate.Collect(ctx, opts)
		if err != nil {
			return Result{}, fmt.Errorf("%s source: %w", candidate.Name(), err)
		}
		return result, nil
	}
	return Result{}, fmt.Errorf("no diff source available; probed %s", strings.Join(probes, "; "))
}

func (r Registry) Names() []string {
	names := make([]string, 0, len(r.sources))
	for _, candidate := range r.sources {
		names = append(names, candidate.Name())
	}
	return names
}

func RepoRoot(ctx context.Context, dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return dir
	}
	out, err := (gitexec.ExecRunner{}).Run(ctx, abs, "rev-parse", "--show-toplevel")
	if err != nil {
		return abs
	}
	return strings.TrimSpace(string(out))
}

var unsafeName = regexp.MustCompile(`[^\w.-]+`)

func SafeName(name string) string {
	name = strings.Trim(unsafeName.ReplaceAllString(name, "-"), "-.")
	if name == "" {
		return "review"
	}
	return name
}
