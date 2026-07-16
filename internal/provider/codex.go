package provider

import (
	"context"
	"fmt"
	"os"
	"os/exec"
)

type CodexCLI struct {
	Model string
}

func (p *CodexCLI) Name() string { return "codex-cli" }

func (p *CodexCLI) Detect() (bool, string) {
	path, err := exec.LookPath("codex")
	if err != nil {
		return false, "codex executable not found"
	}
	return true, "found " + path
}

func (p *CodexCLI) Complete(ctx context.Context, prompt string) (string, error) {
	isolatedDir, err := os.MkdirTemp("", "better-git-review-codex-workspace-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(isolatedDir)
	temp, err := os.CreateTemp("", "better-git-review-codex-*.txt")
	if err != nil {
		return "", err
	}
	outputPath := temp.Name()
	if err := temp.Close(); err != nil {
		return "", err
	}
	defer os.Remove(outputPath)

	args := []string{
		"exec",
		"--ephemeral",
		"--ignore-user-config",
		"--ignore-rules",
		"--skip-git-repo-check",
		"--sandbox", "read-only",
		"--output-last-message", outputPath,
		"-C", isolatedDir,
	}
	if p.Model != "" && p.Model != "default" {
		args = append(args, "--model", p.Model)
	}
	args = append(args, "-")
	if _, err := runCommand(ctx, isolatedDir, []byte(prompt), "codex", args...); err != nil {
		return "", err
	}
	output, err := os.ReadFile(outputPath)
	if err != nil {
		return "", fmt.Errorf("read codex last message: %w", err)
	}
	return string(output), nil
}
