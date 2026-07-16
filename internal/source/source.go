package source

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/janiorvalle/better-git-review/internal/document"
)

type Options struct {
	PR       string
	DiffFile string
	Base     string
	RepoDir  string
	Stdin    io.Reader
	Logf     func(string, ...any)
}

type Result struct {
	Source document.Source
	Diff   []byte
}

func Collect(ctx context.Context, opts Options) (Result, error) {
	repoDir, err := filepath.Abs(opts.RepoDir)
	if err != nil {
		return Result{}, err
	}
	if opts.Logf == nil {
		opts.Logf = func(string, ...any) {}
	}
	if opts.PR != "" {
		return collectPR(ctx, repoDir, opts)
	}
	if opts.DiffFile != "" {
		return collectPatch(repoDir, opts)
	}
	return collectGit(ctx, repoDir, opts)
}

func RepoRoot(ctx context.Context, dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return dir
	}
	out, err := run(ctx, abs, nil, "git", "rev-parse", "--show-toplevel")
	if err != nil {
		return abs
	}
	return strings.TrimSpace(string(out))
}

func collectPR(ctx context.Context, repoDir string, opts Options) (Result, error) {
	opts.Logf("fetching PR #%s via gh ...", opts.PR)
	metaRaw, err := run(ctx, repoDir, nil, "gh", "pr", "view", opts.PR, "--json",
		"number,title,body,baseRefName,headRefName,url")
	if err != nil {
		return Result{}, fmt.Errorf("gh pr view: %w", err)
	}
	var meta struct {
		Number      int    `json:"number"`
		Title       string `json:"title"`
		Body        string `json:"body"`
		BaseRefName string `json:"baseRefName"`
		HeadRefName string `json:"headRefName"`
		URL         string `json:"url"`
	}
	if err := json.Unmarshal(metaRaw, &meta); err != nil {
		return Result{}, fmt.Errorf("parse gh PR metadata: %w", err)
	}
	diffBytes, err := run(ctx, repoDir, nil, "gh", "pr", "diff", opts.PR)
	if err != nil {
		return Result{}, fmt.Errorf("gh pr diff: %w", err)
	}
	url := meta.URL
	return Result{
		Diff: diffBytes,
		Source: document.Source{
			Title:       fmt.Sprintf("PR #%d: %s", meta.Number, meta.Title),
			Description: meta.Body,
			Range:       fmt.Sprintf("%s \u2190 %s", meta.BaseRefName, meta.HeadRefName),
			URL:         &url,
			Name:        fmt.Sprintf("pr-%d", meta.Number),
			RepoDir:     repoDir,
		},
	}, nil
}

func collectPatch(repoDir string, opts Options) (Result, error) {
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
		return Result{}, fmt.Errorf("read diff %q: %w", opts.DiffFile, err)
	}
	return Result{
		Diff: diffBytes,
		Source: document.Source{
			Title:   title,
			Range:   rangeText,
			Name:    sanitizeName(name),
			RepoDir: "",
		},
	}, nil
}

func collectGit(ctx context.Context, repoDir string, opts Options) (Result, error) {
	base := opts.Base
	var err error
	if base == "" {
		base, err = DetectBase(ctx, repoDir)
		if err != nil {
			return Result{}, err
		}
	}
	opts.Logf("diffing %s...HEAD in %s ...", base, repoDir)
	diffBytes, err := run(ctx, repoDir, nil, "git", "diff", base+"...HEAD")
	if err != nil {
		return Result{}, fmt.Errorf("git diff %s...HEAD: %w", base, err)
	}
	rangeText := base + "...HEAD"
	if len(bytes.TrimSpace(diffBytes)) == 0 {
		opts.Logf("no committed changes vs %s; falling back to uncommitted changes (git diff HEAD)", base)
		diffBytes, err = run(ctx, repoDir, nil, "git", "diff", "HEAD")
		if err != nil {
			return Result{}, fmt.Errorf("git diff HEAD: %w", err)
		}
		untracked, untrackedErr := collectUntracked(ctx, repoDir)
		if untrackedErr != nil {
			return Result{}, untrackedErr
		}
		if len(untracked) > 0 {
			if len(diffBytes) > 0 && diffBytes[len(diffBytes)-1] != '\n' {
				diffBytes = append(diffBytes, '\n')
			}
			diffBytes = append(diffBytes, untracked...)
		}
		rangeText = "HEAD (uncommitted)"
	}
	branch := "HEAD"
	if out, branchErr := run(ctx, repoDir, nil, "git", "rev-parse", "--abbrev-ref", "HEAD"); branchErr == nil {
		branch = strings.TrimSpace(string(out))
	}
	repoName := filepath.Base(repoDir)
	return Result{
		Diff: diffBytes,
		Source: document.Source{
			Title:   fmt.Sprintf("%s: %s vs %s", repoName, branch, base),
			Range:   rangeText,
			Name:    sanitizeName(repoName + "-" + branch),
			RepoDir: repoDir,
		},
	}, nil
}

func DetectBase(ctx context.Context, repoDir string) (string, error) {
	if out, err := run(ctx, repoDir, nil, "git", "symbolic-ref", "--quiet", "refs/remotes/origin/HEAD"); err == nil {
		ref := strings.TrimSpace(string(out))
		if ref != "" {
			return strings.TrimPrefix(ref, "refs/remotes/"), nil
		}
	}
	for _, candidate := range []string{"origin/main", "origin/master", "main", "master"} {
		if _, err := run(ctx, repoDir, nil, "git", "rev-parse", "--verify", "--quiet", candidate); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("could not auto-detect a base branch; pass --base <ref>")
}

func run(ctx context.Context, cwd string, stdin []byte, name string, args ...string) ([]byte, error) {
	return runWithExitCodes(ctx, cwd, stdin, []int{0}, name, args...)
}

func runWithExitCodes(
	ctx context.Context,
	cwd string,
	stdin []byte,
	allowedExitCodes []int,
	name string,
	args ...string,
) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = cwd
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			for _, allowed := range allowedExitCodes {
				if exitErr.ExitCode() == allowed {
					return stdout.Bytes(), nil
				}
			}
		}
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = err.Error()
		}
		return nil, fmt.Errorf("%s", detail)
	}
	return stdout.Bytes(), nil
}

func collectUntracked(ctx context.Context, repoDir string) ([]byte, error) {
	rawPaths, err := run(ctx, repoDir, nil, "git", "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, fmt.Errorf("list untracked files: %w", err)
	}
	var output bytes.Buffer
	for _, rawPath := range bytes.Split(rawPaths, []byte{0}) {
		if len(rawPath) == 0 {
			continue
		}
		path := string(rawPath)
		patch, diffErr := runWithExitCodes(
			ctx, repoDir, nil, []int{0, 1},
			"git", "diff", "--no-index", "--", "/dev/null", path,
		)
		if diffErr != nil {
			return nil, fmt.Errorf("diff untracked file %q: %w", path, diffErr)
		}
		if len(patch) == 0 {
			patch, diffErr = emptyFilePatch(repoDir, path)
			if diffErr != nil {
				return nil, diffErr
			}
		}
		output.Write(patch)
		if len(patch) > 0 && patch[len(patch)-1] != '\n' {
			output.WriteByte('\n')
		}
	}
	return output.Bytes(), nil
}

func emptyFilePatch(repoDir, path string) ([]byte, error) {
	info, err := os.Lstat(filepath.Join(repoDir, filepath.FromSlash(path)))
	if err != nil {
		return nil, fmt.Errorf("inspect untracked file %q: %w", path, err)
	}
	mode := "100644"
	if info.Mode()&0o111 != 0 {
		mode = "100755"
	}
	oldPath := gitPatchPath("a", path)
	newPath := gitPatchPath("b", path)
	return []byte(fmt.Sprintf(
		"diff --git %s %s\nnew file mode %s\nindex 0000000..e69de29\n--- /dev/null\n+++ %s\n",
		oldPath, newPath, mode, newPath,
	)), nil
}

func gitPatchPath(side, path string) string {
	fullPath := side + "/" + path
	if strings.ContainsAny(fullPath, "\t\n\"\\") {
		return strconv.Quote(fullPath)
	}
	return fullPath
}

var unsafeName = regexp.MustCompile(`[^\w.-]+`)

func sanitizeName(name string) string {
	name = strings.Trim(unsafeName.ReplaceAllString(name, "-"), "-.")
	if name == "" {
		return "review"
	}
	return name
}
