package gitexec

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

type Runner interface {
	Run(ctx context.Context, repoDir string, args ...string) ([]byte, error)
}

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, repoDir string, args ...string) ([]byte, error) {
	if len(args) < 2 || args[0] != "-c" || args[1] != "color.ui=false" {
		args = Harden(args...)
	}
	command := exec.CommandContext(ctx, "git", args...)
	command.Dir = repoDir
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

func Harden(args ...string) []string {
	result := make([]string, 0, len(args)+2)
	result = append(result, "-c", "color.ui=false")
	result = append(result, args...)
	return result
}

func DiffArgs(target string) []string {
	return DiffArgsWithOptions(target, 0, 0)
}

func DiffArgsWithOptions(target string, contextLines, findRenames int) []string {
	args := []string{
		"-c", "diff.mnemonicPrefix=false",
		"diff",
		"--no-ext-diff",
		"--no-textconv",
		"--no-color",
		"--src-prefix=a/",
		"--dst-prefix=b/",
	}
	if contextLines > 0 {
		args = append(args, fmt.Sprintf("-U%d", contextLines))
	}
	if findRenames > 0 {
		args = append(args, fmt.Sprintf("-M%d%%", findRenames))
	}
	return append(args, target)
}

func RootDiffArgs(commit string) []string {
	return RootDiffArgsWithOptions(commit, 0, 0)
}

func RootDiffArgsWithOptions(commit string, contextLines, findRenames int) []string {
	args := []string{
		"-c", "diff.mnemonicPrefix=false",
		"diff-tree",
		"--root",
		"--no-commit-id",
		"--patch",
		"--no-ext-diff",
		"--no-textconv",
		"--no-color",
		"--src-prefix=a/",
		"--dst-prefix=b/",
	}
	if contextLines > 0 {
		args = append(args, fmt.Sprintf("-U%d", contextLines))
	}
	if findRenames > 0 {
		args = append(args, fmt.Sprintf("-M%d%%", findRenames))
	}
	return append(args, commit)
}

func BlameArgs(ref, lineRange, path string) []string {
	if ref == "" {
		ref = "HEAD"
	}
	return []string{
		"blame", "--porcelain", "--no-textconv",
		"-L", lineRange,
		ref, "--", path,
	}
}
