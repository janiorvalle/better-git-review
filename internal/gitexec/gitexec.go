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
	return []string{
		"-c", "diff.mnemonicPrefix=false",
		"diff",
		"--no-ext-diff",
		"--no-textconv",
		"--no-color",
		"--src-prefix=a/",
		"--dst-prefix=b/",
		target,
	}
}

func BlameArgs(lineRange, path string) []string {
	return []string{
		"blame", "--porcelain", "--no-textconv",
		"-L", lineRange,
		"HEAD", "--", path,
	}
}
