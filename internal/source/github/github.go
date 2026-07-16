package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/janiorvalle/better-git-review/internal/document"
	"github.com/janiorvalle/better-git-review/internal/source"
)

type Source struct{}

func (Source) Name() string {
	return "github"
}

func (Source) Detect(opts source.Options) (bool, string) {
	if opts.PR == "" {
		return false, "no PR number requested"
	}
	return true, "PR number requested"
}

func (Source) Collect(ctx context.Context, opts source.Options) (source.Result, error) {
	opts.Logf("fetching PR #%s via gh ...", opts.PR)
	metaRaw, err := run(ctx, opts.RepoDir, "gh", "pr", "view", opts.PR, "--json",
		"number,title,body,baseRefName,headRefName,url")
	if err != nil {
		return source.Result{}, fmt.Errorf("gh pr view: %w", err)
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
		return source.Result{}, fmt.Errorf("parse gh PR metadata: %w", err)
	}
	diffBytes, err := run(ctx, opts.RepoDir, "gh", "pr", "diff", opts.PR, "--color", "never")
	if err != nil {
		return source.Result{}, fmt.Errorf("gh pr diff: %w", err)
	}
	url := meta.URL
	return source.Result{
		Diff: diffBytes,
		Source: document.Source{
			Title:       fmt.Sprintf("PR #%d: %s", meta.Number, meta.Title),
			Description: meta.Body,
			Range:       fmt.Sprintf("%s \u2190 %s", meta.BaseRefName, meta.HeadRefName),
			URL:         &url,
			Name:        fmt.Sprintf("pr-%d", meta.Number),
			RepoDir:     opts.RepoDir,
		},
	}, nil
}

func run(ctx context.Context, cwd, name string, args ...string) ([]byte, error) {
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
