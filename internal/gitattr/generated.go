package gitattr

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/janiorvalle/better-git-review/internal/document"
)

type Runner interface {
	Check(context.Context, string, string, []string) ([]byte, error)
}

type ExecRunner struct{}

func (ExecRunner) Check(ctx context.Context, repoDir, ref string, paths []string) ([]byte, error) {
	args := []string{
		"-c", "color.ui=false",
		"check-attr", "-z",
	}
	if ref != "" {
		args = append(args, "--source="+ref)
	}
	args = append(args, "--stdin", "linguist-generated")
	command := exec.CommandContext(ctx, "git", args...)
	command.Dir = repoDir
	var input, stdout, stderr bytes.Buffer
	for _, path := range paths {
		input.WriteString(path)
		input.WriteByte(0)
	}
	command.Stdin = &input
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = err.Error()
		}
		return nil, fmt.Errorf("%s", firstLineDiagnostic(detail))
	}
	return stdout.Bytes(), nil
}

func firstLineDiagnostic(detail string) string {
	detail = strings.TrimSpace(detail)
	line, _, _ := strings.Cut(detail, "\n")
	line = strings.TrimSuffix(line, "\r")
	diagnostic := strconv.QuoteToASCII(line)
	if len(diagnostic) <= 200 {
		return diagnostic
	}
	const suffix = "... [truncated]"
	var escaped strings.Builder
	budget := 200 - len(suffix) - 2
	for _, value := range line {
		quoted := strconv.QuoteToASCII(string(value))
		encoded := quoted[1 : len(quoted)-1]
		if escaped.Len()+len(encoded) > budget {
			break
		}
		escaped.WriteString(encoded)
	}
	return `"` + escaped.String() + suffix + `"`
}

func Generated(
	ctx context.Context,
	repoDir, ref string,
	files []document.File,
	runner Runner,
	logf func(string, ...any),
) map[int]bool {
	result := map[int]bool{}
	if repoDir == "" || ref == "" {
		return result
	}
	if runner == nil {
		runner = ExecRunner{}
	}
	paths := make([]string, len(files))
	indexesByPath := make(map[string][]int, len(files))
	for index, file := range files {
		paths[index] = file.Path
		indexesByPath[file.Path] = append(indexesByPath[file.Path], index)
	}
	output, err := runner.Check(ctx, repoDir, ref, paths)
	if err != nil {
		if logf != nil {
			logf("could not read attributes from the reviewed commit; using worktree attributes")
		}
		output, err = runner.Check(ctx, repoDir, "", paths)
		if err != nil {
			if logf != nil {
				logf("could not read linguist-generated attributes; keeping files review-worthy: %v", err)
			}
			return result
		}
	}
	fields := bytes.Split(output, []byte{0})
	for offset := 0; offset+2 < len(fields); offset += 3 {
		path := string(fields[offset])
		attribute := string(fields[offset+1])
		value := strings.ToLower(string(fields[offset+2]))
		if attribute != "linguist-generated" || (value != "set" && value != "true") {
			continue
		}
		for _, index := range indexesByPath[path] {
			result[index] = true
		}
	}
	return result
}
