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
		baseName  = opts.Base
		headName  = opts.Head
		baseRef   string
		headRef   string
		diffBytes []byte
		rangeText string
		err       error
	)
	if headName == "" {
		headName = "HEAD"
	}
	branchRef := headName
	if opts.Dirty {
		headRef, err = resolveCommit(ctx, opts.RepoDir, "HEAD", runner)
		if err != nil {
			return source.Result{}, err
		}
		opts.Logf("diffing uncommitted changes in %s (git diff HEAD) ...", opts.RepoDir)
		diffBytes, err = runner.Run(ctx, opts.RepoDir, gitexec.DiffArgs(headRef)...)
		if err != nil {
			return source.Result{}, fmt.Errorf("git diff HEAD: %w", err)
		}
		rangeText = "HEAD (uncommitted)"
		baseRef, headRef = headRef, "WORKTREE"
	} else if opts.Commit != "" {
		headRef, err = resolveCommit(ctx, opts.RepoDir, opts.Commit, runner)
		if err != nil {
			return source.Result{}, err
		}
		branchRef = headRef
		rangeText = opts.Commit + "^.." + opts.Commit
		opts.Logf("diffing commit %s in %s ...", opts.Commit, opts.RepoDir)
		parent, root, parentErr := commitParent(ctx, opts.RepoDir, headRef, runner)
		if parentErr != nil {
			return source.Result{}, parentErr
		}
		if !root {
			baseRef = parent
			diffBytes, err = runner.Run(ctx, opts.RepoDir, gitexec.DiffArgs(baseRef+".."+headRef)...)
		} else {
			baseRef = ""
			rangeText = opts.Commit + " (root commit)"
			diffBytes, err = runner.Run(ctx, opts.RepoDir, gitexec.RootDiffArgs(headRef)...)
		}
		if err != nil {
			return source.Result{}, fmt.Errorf("git diff commit %s: %w", opts.Commit, err)
		}
	} else {
		if baseName == "" {
			baseName, err = DetectBase(ctx, opts.RepoDir, runner)
			if err != nil {
				return source.Result{}, err
			}
		}
		baseRef, err = resolveCommit(ctx, opts.RepoDir, baseName, runner)
		if err != nil {
			return source.Result{}, err
		}
		headRef, err = resolveCommit(ctx, opts.RepoDir, headName, runner)
		if err != nil {
			return source.Result{}, err
		}
		opts.Logf("diffing %s...%s in %s ...", baseName, headName, opts.RepoDir)
		diffBytes, err = runner.Run(ctx, opts.RepoDir, gitexec.DiffArgs(baseRef+"..."+headRef)...)
		if err != nil {
			return source.Result{}, fmt.Errorf("git diff %s...%s: %w", baseName, headName, err)
		}
		rangeText = baseName + "..." + headName
		if len(bytes.TrimSpace(diffBytes)) == 0 && opts.Head == "" {
			opts.Logf("no committed changes vs %s; falling back to uncommitted changes (git diff HEAD)", baseName)
			diffBytes, err = runner.Run(ctx, opts.RepoDir, gitexec.DiffArgs(headRef)...)
			if err != nil {
				return source.Result{}, fmt.Errorf("git diff HEAD: %w", err)
			}
			rangeText = "HEAD (uncommitted)"
			baseRef, headRef = headRef, "WORKTREE"
		}
	}
	branch := branchRef
	if out, branchErr := runner.Run(ctx, opts.RepoDir, "rev-parse", "--abbrev-ref", branchRef); branchErr == nil {
		branch = strings.TrimSpace(string(out))
	}
	repoName := filepath.Base(opts.RepoDir)
	title := fmt.Sprintf("%s: %s vs %s", repoName, branch, baseName)
	if opts.Dirty {
		title = fmt.Sprintf("%s: %s uncommitted changes", repoName, branch)
	} else if opts.Commit != "" {
		title = fmt.Sprintf("%s: commit %s", repoName, opts.Commit)
	}
	return source.Result{
		Diff:    diffBytes,
		BaseRef: baseRef,
		HeadRef: headRef,
		Dirty:   opts.Dirty || rangeText == "HEAD (uncommitted)",
		Source: document.Source{
			Title:   title,
			Range:   rangeText,
			Name:    source.SafeName(repoName + "-" + branch),
			RepoDir: opts.RepoDir,
		},
	}, nil
}

func resolveCommit(ctx context.Context, repoDir, ref string, runner gitexec.Runner) (string, error) {
	resolved, err := runner.Run(ctx, repoDir, "rev-parse", "--verify", ref+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("resolve git ref %s: %w", ref, err)
	}
	return strings.TrimSpace(string(resolved)), nil
}

func commitParent(ctx context.Context, repoDir, commit string, runner gitexec.Runner) (string, bool, error) {
	content, err := runner.Run(ctx, repoDir, "cat-file", "-p", commit)
	if err != nil {
		return "", false, fmt.Errorf("read commit %s: %w", commit, err)
	}
	for _, line := range strings.Split(string(content), "\n") {
		if line == "" {
			break
		}
		parent, found := strings.CutPrefix(line, "parent ")
		if !found {
			continue
		}
		parent = strings.TrimSpace(parent)
		if _, err := runner.Run(ctx, repoDir, "cat-file", "-e", parent+"^{commit}"); err != nil {
			return "", false, fmt.Errorf("parent %s for commit %s is unavailable; deepen or fetch the clone before reviewing this commit", parent, commit)
		}
		return parent, false, nil
	}
	return "", true, nil
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
