package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/janiorvalle/better-git-review/internal/document"
	"github.com/janiorvalle/better-git-review/internal/gitexec"
	"github.com/janiorvalle/better-git-review/internal/source"
)

const forgeLessHint = "Not using GitHub? Review by ref instead: --base/--head, --commit, --dirty, or pipe any diff via --diff -."

type Runner interface {
	LookPath(string) (string, error)
	Run(context.Context, string, string, ...string) ([]byte, error)
}

type Source struct {
	Runner       Runner
	GitRunner    gitexec.Runner
	RetryBackoff time.Duration
	Sleep        func(context.Context, time.Duration) error
}

func (Source) Name() string { return "github" }

func (s Source) Detect(opts source.Options) (bool, string) {
	if opts.PR == "" {
		return false, "no PR number requested"
	}
	runner := s.commandRunner()
	if _, err := runner.LookPath("gh"); err != nil {
		return false, "PR mode needs the GitHub CLI - install from cli.github.com, then `gh auth login`. " + forgeLessHint
	}
	if _, err := runner.Run(context.Background(), opts.RepoDir, "gh", "auth", "status", "--active"); err != nil {
		return false, "GitHub CLI is not authenticated - run `gh auth login`. " + forgeLessHint
	}
	return true, "GitHub CLI is installed and authenticated"
}

func (s Source) Collect(ctx context.Context, opts source.Options) (source.Result, error) {
	runner := s.commandRunner()
	opts.Logf("fetching PR #%s from GitHub ...", opts.PR)
	metaRaw, err := runner.Run(ctx, opts.RepoDir, "gh", "pr", "view", opts.PR, "--json",
		"number,title,body,baseRefName,headRefName,baseRefOid,headRefOid,changedFiles,url")
	if err != nil {
		return source.Result{}, fmt.Errorf("gh pr view: %w", err)
	}
	var meta struct {
		Number       int    `json:"number"`
		Title        string `json:"title"`
		Body         string `json:"body"`
		BaseRefName  string `json:"baseRefName"`
		HeadRefName  string `json:"headRefName"`
		BaseRefOID   string `json:"baseRefOid"`
		HeadRefOID   string `json:"headRefOid"`
		ChangedFiles int    `json:"changedFiles"`
		URL          string `json:"url"`
	}
	if err := json.Unmarshal(metaRaw, &meta); err != nil {
		return source.Result{}, fmt.Errorf("parse gh PR metadata: %w", err)
	}

	var (
		diffBytes  []byte
		diffErr    error
		fallbackOn string
	)
	maxFiles := opts.GitHubPRDiffMaxFiles
	if maxFiles == 0 {
		maxFiles = 300
	}
	if opts.GitContextLines > 0 || opts.GitFindRenames > 0 {
		fallbackOn = "configured Git diff settings require local git objects"
	} else if meta.ChangedFiles > maxFiles {
		fallbackOn = fmt.Sprintf("GitHub reports %d changed files", meta.ChangedFiles)
	} else {
		diffBytes, diffErr = s.diffWithRetry(ctx, runner, opts)
		if diffErr != nil {
			fallbackOn = "gh pr diff failed: " + diffErr.Error()
		}
	}
	if fallbackOn != "" {
		opts.Logf("%s - using local git objects instead ...", fallbackOn)
		diffBytes, err = s.localDiff(ctx, opts.RepoDir, meta.URL, meta.BaseRefOID, meta.HeadRefOID, opts.GitContextLines, opts.GitFindRenames)
		if err != nil {
			return source.Result{}, fmt.Errorf("%s; local-git fallback failed: %w. Run PR mode from a clone with a remote matching the PR repository", fallbackOn, err)
		}
	} else if err := s.fetchObjects(ctx, opts.RepoDir, meta.URL, meta.BaseRefOID, meta.HeadRefOID, false); err != nil {
		opts.Logf("local PR objects unavailable; binary previews may be limited")
	}

	url := meta.URL
	return source.Result{
		Diff:    diffBytes,
		BaseRef: meta.BaseRefOID,
		HeadRef: meta.HeadRefOID,
		Source: document.Source{
			Title:       fmt.Sprintf("PR #%d: %s", meta.Number, meta.Title),
			Description: meta.Body,
			Range:       fmt.Sprintf("%s ← %s", meta.BaseRefName, meta.HeadRefName),
			URL:         &url,
			Name:        fmt.Sprintf("pr-%d", meta.Number),
			RepoDir:     opts.RepoDir,
		},
	}, nil
}

func (s Source) diffWithRetry(ctx context.Context, runner Runner, opts source.Options) ([]byte, error) {
	backoff := s.RetryBackoff
	if backoff == 0 {
		backoff = 150 * time.Millisecond
	}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		result, err := runner.Run(ctx, opts.RepoDir, "gh", "pr", "diff", opts.PR, "--color", "never")
		if err == nil {
			return result, nil
		}
		lastErr = err
		if !isServerError(err) || attempt == 2 {
			break
		}
		if err := s.sleep(ctx, backoff*time.Duration(attempt+1)); err != nil {
			return nil, err
		}
	}
	return nil, lastErr
}

func (s Source) localDiff(ctx context.Context, repoDir, prURL, baseOID, headOID string, diffOptions ...int) ([]byte, error) {
	if err := s.fetchObjects(ctx, repoDir, prURL, baseOID, headOID, true); err != nil {
		return nil, err
	}
	runner := s.gitRunner()
	contextLines, findRenames := 0, 0
	if len(diffOptions) > 0 {
		contextLines = diffOptions[0]
	}
	if len(diffOptions) > 1 {
		findRenames = diffOptions[1]
	}
	diffBytes, err := runner.Run(ctx, repoDir, gitexec.DiffArgsWithOptions(baseOID+"..."+headOID, contextLines, findRenames)...)
	if err != nil {
		return nil, fmt.Errorf("git diff %s...%s: %w", baseOID, headOID, err)
	}
	return diffBytes, nil
}

func (s Source) fetchObjects(ctx context.Context, repoDir, prURL, baseOID, headOID string, force bool) error {
	if baseOID == "" || headOID == "" {
		return fmt.Errorf("PR metadata did not include base/head object IDs")
	}
	runner := s.gitRunner()
	if _, err := runner.Run(ctx, repoDir, "rev-parse", "--is-inside-work-tree"); err != nil {
		return fmt.Errorf("%q is not a usable local git repository: %w", repoDir, err)
	}
	basePresent := objectExists(ctx, runner, repoDir, baseOID)
	headPresent := objectExists(ctx, runner, repoDir, headOID)
	if !force && basePresent && headPresent {
		return nil
	}
	remote, err := matchingRemote(ctx, runner, repoDir, prURL)
	if err != nil {
		return err
	}
	if _, err := runner.Run(ctx, repoDir, "fetch", "--no-tags", remote, baseOID, headOID); err != nil {
		return fmt.Errorf("git fetch %s <PR object IDs>: %w", remote, err)
	}
	return nil
}

func objectExists(ctx context.Context, runner gitexec.Runner, repoDir, oid string) bool {
	_, err := runner.Run(ctx, repoDir, "cat-file", "-e", oid+"^{commit}")
	return err == nil
}

func (s Source) gitRunner() gitexec.Runner {
	if s.GitRunner != nil {
		return s.GitRunner
	}
	return gitexec.ExecRunner{}
}

func matchingRemote(ctx context.Context, runner gitexec.Runner, repoDir, prURL string) (string, error) {
	remoteRaw, err := runner.Run(ctx, repoDir, "remote")
	if err != nil {
		return "", fmt.Errorf("list git remotes: %w", err)
	}
	remotes := strings.Fields(string(remoteRaw))
	if len(remotes) == 0 {
		return "", fmt.Errorf("local repository has no git remotes")
	}
	target := repositoryIdentity(prURL)
	if target != "" {
		for _, remote := range remotes {
			remoteURL, remoteErr := runner.Run(ctx, repoDir, "remote", "get-url", remote)
			if remoteErr == nil && strings.EqualFold(repositoryIdentity(strings.TrimSpace(string(remoteURL))), target) {
				return remote, nil
			}
		}
	}
	if len(remotes) == 1 {
		return remotes[0], nil
	}
	return "", fmt.Errorf("no git remote matches PR repository %q", target)
}

func repositoryIdentity(raw string) string {
	raw = strings.TrimSpace(raw)
	if !strings.Contains(raw, "://") {
		if marker := strings.Index(raw, ":"); marker > 0 {
			host := raw[:marker]
			if at := strings.LastIndex(host, "@"); at >= 0 {
				host = host[at+1:]
			}
			if slug := cleanRepositorySlug(raw[marker+1:]); host != "" && slug != "" {
				return strings.ToLower(host) + "/" + slug
			}
		}
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" {
		return ""
	}
	slug := cleanRepositorySlug(parsed.Path)
	if slug == "" {
		return ""
	}
	return strings.ToLower(parsed.Hostname()) + "/" + slug
}

func cleanRepositorySlug(path string) string {
	parts := strings.Split(strings.Trim(strings.TrimSuffix(path, ".git"), "/"), "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return ""
	}
	return parts[0] + "/" + parts[1]
}

func (s Source) commandRunner() Runner {
	if s.Runner != nil {
		return s.Runner
	}
	return execRunner{}
}

func (s Source) sleep(ctx context.Context, duration time.Duration) error {
	if s.Sleep != nil {
		return s.Sleep(ctx, duration)
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

var serverErrorPattern = regexp.MustCompile(`(?i)(HTTP[^\n]*\b5\d\d\b|\b5\d\d\s+(internal|bad gateway|service unavailable|gateway timeout))`)

func isServerError(err error) bool {
	return err != nil && serverErrorPattern.MatchString(err.Error())
}

type execRunner struct{}

func (execRunner) LookPath(name string) (string, error) { return exec.LookPath(name) }

func (execRunner) Run(ctx context.Context, cwd, name string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, name, args...)
	command.Dir = cwd
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = err.Error()
		}
		return nil, fmt.Errorf("%s", detail)
	}
	return stdout.Bytes(), nil
}
