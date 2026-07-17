package blame

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/janiorvalle/better-git-review/internal/document"
	"github.com/janiorvalle/better-git-review/internal/gitexec"
)

type Runner = gitexec.Runner

func Enrich(ctx context.Context, repoDir string, files []document.File, runner Runner) {
	EnrichRef(ctx, repoDir, "HEAD", files, runner)
}

func EnrichRef(ctx context.Context, repoDir, ref string, files []document.File, runner Runner) {
	enrich(ctx, repoDir, ref, files, newLineRange, runner)
}

func EnrichUncommitted(ctx context.Context, repoDir string, files []document.File, runner Runner) {
	enrich(ctx, repoDir, "HEAD", files, oldLineRange, runner)
}

func enrich(
	ctx context.Context,
	repoDir string,
	ref string,
	files []document.File,
	lineRange func(document.Hunk) (int, int, bool),
	runner Runner,
) {
	if repoDir == "" {
		return
	}
	if runner == nil {
		runner = gitexec.ExecRunner{}
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
				gitexec.Harden(gitexec.BlameArgs(ref, fmt.Sprintf("%d,%d", start, end), file.Path)...)...)
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
		author      string
		authorTZ    string
		authorTS    int64
		committerTZ string
		committerTS int64
		best        *document.Blame
		bestStamp   int64
	)
	flush := func() {
		stamp, timezone := committerTS, committerTZ
		if stamp == 0 {
			stamp, timezone = authorTS, authorTZ
		}
		if author == "" || stamp == 0 || stamp < bestStamp {
			return
		}
		location := time.UTC
		if len(timezone) == 5 {
			sign := 1
			if timezone[0] == '-' {
				sign = -1
			}
			hours, hourErr := strconv.Atoi(timezone[1:3])
			minutes, minuteErr := strconv.Atoi(timezone[3:5])
			if hourErr == nil && minuteErr == nil {
				location = time.FixedZone("", sign*(hours*60+minutes)*60)
			}
		}
		bestStamp = stamp
		best = &document.Blame{
			Author: author,
			Date:   time.Unix(stamp, 0).In(location).Format(time.RFC3339),
		}
	}

	for _, line := range strings.Split(string(output), "\n") {
		if isCommitHeader(line) {
			flush()
			author, authorTZ, authorTS = "", "", 0
			committerTZ, committerTS = "", 0
			continue
		}
		switch {
		case strings.HasPrefix(line, "author "):
			author = strings.TrimPrefix(line, "author ")
		case strings.HasPrefix(line, "author-time "):
			authorTS, _ = strconv.ParseInt(strings.TrimPrefix(line, "author-time "), 10, 64)
		case strings.HasPrefix(line, "author-tz "):
			authorTZ = strings.TrimPrefix(line, "author-tz ")
		case strings.HasPrefix(line, "committer-time "):
			committerTS, _ = strconv.ParseInt(strings.TrimPrefix(line, "committer-time "), 10, 64)
		case strings.HasPrefix(line, "committer-tz "):
			committerTZ = strings.TrimPrefix(line, "committer-tz ")
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
