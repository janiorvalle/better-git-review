package blame

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/janiorvalle/better-git-review/internal/document"
)

type Runner interface {
	Run(ctx context.Context, repoDir string, args ...string) ([]byte, error)
}

type GitRunner struct{}

func (GitRunner) Run(ctx context.Context, repoDir string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, "git", args...)
	command.Dir = repoDir
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return nil, fmt.Errorf("%s", strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

func Enrich(ctx context.Context, repoDir string, files []document.File, runner Runner) {
	enrich(ctx, repoDir, files, newLineRange, runner)
}

func EnrichUncommitted(ctx context.Context, repoDir string, files []document.File, runner Runner) {
	enrich(ctx, repoDir, files, oldLineRange, runner)
}

func enrich(
	ctx context.Context,
	repoDir string,
	files []document.File,
	lineRange func(document.Hunk) (int, int, bool),
	runner Runner,
) {
	if repoDir == "" {
		return
	}
	if runner == nil {
		runner = GitRunner{}
	}
	for fileIndex := range files {
		file := &files[fileIndex]
		if file.Binary || file.Status == "deleted" {
			continue
		}
		for hunkIndex := range file.Hunks {
			start, end, ok := lineRange(file.Hunks[hunkIndex])
			if !ok {
				continue
			}
			output, err := runner.Run(ctx, repoDir,
				"-c", "color.ui=false",
				"blame", "--porcelain", "--no-textconv",
				"-L", fmt.Sprintf("%d,%d", start, end),
				"HEAD", "--", file.Path,
			)
			if err != nil {
				continue
			}
			if parsed, parseErr := ParsePorcelain(output); parseErr == nil {
				file.Hunks[hunkIndex].Blame = parsed
			}
		}
	}
}

func ParsePorcelain(output []byte) (*document.Blame, error) {
	var (
		author    string
		authorTZ  string
		authorTS  int64
		best      *document.Blame
		bestStamp int64
	)
	flush := func() {
		if author == "" || authorTS == 0 || authorTS < bestStamp {
			return
		}
		location := time.UTC
		if len(authorTZ) == 5 {
			sign := 1
			if authorTZ[0] == '-' {
				sign = -1
			}
			hours, hourErr := strconv.Atoi(authorTZ[1:3])
			minutes, minuteErr := strconv.Atoi(authorTZ[3:5])
			if hourErr == nil && minuteErr == nil {
				location = time.FixedZone("", sign*(hours*60+minutes)*60)
			}
		}
		bestStamp = authorTS
		best = &document.Blame{
			Author: author,
			Date:   time.Unix(authorTS, 0).In(location).Format(time.RFC3339),
		}
	}

	for _, line := range strings.Split(string(output), "\n") {
		if isCommitHeader(line) {
			flush()
			author, authorTZ, authorTS = "", "", 0
			continue
		}
		switch {
		case strings.HasPrefix(line, "author "):
			author = strings.TrimPrefix(line, "author ")
		case strings.HasPrefix(line, "author-time "):
			authorTS, _ = strconv.ParseInt(strings.TrimPrefix(line, "author-time "), 10, 64)
		case strings.HasPrefix(line, "author-tz "):
			authorTZ = strings.TrimPrefix(line, "author-tz ")
		}
	}
	flush()
	if best == nil {
		return nil, fmt.Errorf("blame output contained no author metadata")
	}
	return best, nil
}

func newLineRange(hunk document.Hunk) (int, int, bool) {
	start, end := 0, 0
	for _, line := range hunk.Lines {
		if line.New == 0 {
			continue
		}
		if start == 0 || line.New < start {
			start = line.New
		}
		if line.New > end {
			end = line.New
		}
	}
	return start, end, start > 0
}

func oldLineRange(hunk document.Hunk) (int, int, bool) {
	start, end := 0, 0
	for _, line := range hunk.Lines {
		if line.Old == 0 {
			continue
		}
		if start == 0 || line.Old < start {
			start = line.Old
		}
		if line.Old > end {
			end = line.Old
		}
	}
	return start, end, start > 0
}

func isCommitHeader(line string) bool {
	fields := strings.Fields(line)
	if len(fields) < 3 || len(fields[0]) < 8 {
		return false
	}
	for _, char := range fields[0] {
		if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f') || char == '^') {
			return false
		}
	}
	return true
}
