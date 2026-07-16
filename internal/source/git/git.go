package git

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/janiorvalle/better-git-review/internal/document"
	"github.com/janiorvalle/better-git-review/internal/gitexec"
	"github.com/janiorvalle/better-git-review/internal/source"
)

type Source struct {
	Runner gitexec.Runner
}

func (Source) Name() string {
	return "git"
}

func (Source) Detect(opts source.Options) (bool, string) {
	if opts.PR != "" || opts.DiffFile != "" {
		return false, "another source was explicitly requested"
	}
	return true, "local git is the default source"
}

func (s Source) Collect(ctx context.Context, opts source.Options) (source.Result, error) {
	runner := s.Runner
	if runner == nil {
		runner = gitexec.ExecRunner{}
	}
	base := opts.Base
	var err error
	if base == "" {
		base, err = DetectBase(ctx, opts.RepoDir, runner)
		if err != nil {
			return source.Result{}, err
		}
	}
	opts.Logf("diffing %s...HEAD in %s ...", base, opts.RepoDir)
	diffBytes, err := runner.Run(ctx, opts.RepoDir, gitexec.DiffArgs(base+"...HEAD")...)
	if err != nil {
		return source.Result{}, fmt.Errorf("git diff %s...HEAD: %w", base, err)
	}
	rangeText := base + "...HEAD"
	if len(bytes.TrimSpace(diffBytes)) == 0 {
		opts.Logf("no committed changes vs %s; falling back to uncommitted changes (git diff HEAD)", base)
		diffBytes, err = runner.Run(ctx, opts.RepoDir, gitexec.DiffArgs("HEAD")...)
		if err != nil {
			return source.Result{}, fmt.Errorf("git diff HEAD: %w", err)
		}
		rangeText = "HEAD (uncommitted)"
	}
	branch := "HEAD"
	if out, branchErr := runner.Run(ctx, opts.RepoDir, "rev-parse", "--abbrev-ref", "HEAD"); branchErr == nil {
		branch = strings.TrimSpace(string(out))
	}
	repoName := filepath.Base(opts.RepoDir)
	return source.Result{
		Diff: diffBytes,
		Source: document.Source{
			Title:   fmt.Sprintf("%s: %s vs %s", repoName, branch, base),
			Range:   rangeText,
			Name:    source.SafeName(repoName + "-" + branch),
			RepoDir: opts.RepoDir,
		},
	}, nil
}

func DetectBase(ctx context.Context, repoDir string, runner gitexec.Runner) (string, error) {
	if runner == nil {
		runner = gitexec.ExecRunner{}
	}
	if out, err := runner.Run(ctx, repoDir, "symbolic-ref", "--quiet", "refs/remotes/origin/HEAD"); err == nil {
		ref := strings.TrimSpace(string(out))
		if ref != "" {
			candidate := strings.TrimPrefix(ref, "refs/remotes/")
			if _, verifyErr := runner.Run(ctx, repoDir, "rev-parse", "--verify", "--quiet", candidate); verifyErr == nil {
				return candidate, nil
			}
		}
	}
	for _, candidate := range []string{"origin/main", "origin/master", "main", "master"} {
		if _, err := runner.Run(ctx, repoDir, "rev-parse", "--verify", "--quiet", candidate); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("could not auto-detect a base branch; pass --base <ref>")
}
