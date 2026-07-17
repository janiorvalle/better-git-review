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
	var (
		base      = opts.Base
		head      = opts.Head
		diffBytes []byte
		rangeText string
		err       error
	)
	if head == "" {
		head = "HEAD"
	}
	if opts.Dirty {
		opts.Logf("diffing uncommitted changes in %s (git diff HEAD) ...", opts.RepoDir)
		diffBytes, err = runner.Run(ctx, opts.RepoDir, gitexec.DiffArgs("HEAD")...)
		if err != nil {
			return source.Result{}, fmt.Errorf("git diff HEAD: %w", err)
		}
		rangeText = "HEAD (uncommitted)"
		base, head = "HEAD", "WORKTREE"
	} else if opts.Commit != "" {
		head = opts.Commit
		rangeText = opts.Commit + "^.." + head
		opts.Logf("diffing commit %s in %s ...", opts.Commit, opts.RepoDir)
		parent, parentErr := runner.Run(ctx, opts.RepoDir, "rev-parse", "--verify", "--quiet", opts.Commit+"^")
		if parentErr == nil {
			base = strings.TrimSpace(string(parent))
			diffBytes, err = runner.Run(ctx, opts.RepoDir, gitexec.DiffArgs(base+".."+head)...)
		} else {
			base = ""
			rangeText = opts.Commit + " (root commit)"
			diffBytes, err = runner.Run(ctx, opts.RepoDir, gitexec.RootDiffArgs(head)...)
		}
		if err != nil {
			return source.Result{}, fmt.Errorf("git diff commit %s: %w", opts.Commit, err)
		}
	} else {
		if base == "" {
			base, err = DetectBase(ctx, opts.RepoDir, runner)
			if err != nil {
				return source.Result{}, err
			}
		}
		opts.Logf("diffing %s...%s in %s ...", base, head, opts.RepoDir)
		diffBytes, err = runner.Run(ctx, opts.RepoDir, gitexec.DiffArgs(base+"..."+head)...)
		if err != nil {
			return source.Result{}, fmt.Errorf("git diff %s...%s: %w", base, head, err)
		}
		rangeText = base + "..." + head
		if len(bytes.TrimSpace(diffBytes)) == 0 && opts.Head == "" {
			opts.Logf("no committed changes vs %s; falling back to uncommitted changes (git diff HEAD)", base)
			diffBytes, err = runner.Run(ctx, opts.RepoDir, gitexec.DiffArgs("HEAD")...)
			if err != nil {
				return source.Result{}, fmt.Errorf("git diff HEAD: %w", err)
			}
			rangeText = "HEAD (uncommitted)"
			base, head = "HEAD", "WORKTREE"
		}
	}
	branchRef := head
	if opts.Dirty || head == "WORKTREE" {
		branchRef = "HEAD"
	}
	branch := branchRef
	if out, branchErr := runner.Run(ctx, opts.RepoDir, "rev-parse", "--abbrev-ref", branchRef); branchErr == nil {
		branch = strings.TrimSpace(string(out))
	}
	repoName := filepath.Base(opts.RepoDir)
	title := fmt.Sprintf("%s: %s vs %s", repoName, branch, base)
	if opts.Dirty {
		title = fmt.Sprintf("%s: %s uncommitted changes", repoName, branch)
	} else if opts.Commit != "" {
		title = fmt.Sprintf("%s: commit %s", repoName, opts.Commit)
	}
	return source.Result{
		Diff:    diffBytes,
		BaseRef: base,
		HeadRef: head,
		Dirty:   opts.Dirty || rangeText == "HEAD (uncommitted)",
		Source: document.Source{
			Title:   title,
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
