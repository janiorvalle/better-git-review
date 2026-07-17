package blame

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/janiorvalle/better-git-review/internal/document"
)

func TestParsePorcelainUsesMostRecentCommit(t *testing.T) {
	output := `aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa 1 1 1
author Older Author
author-time 1735689600
author-tz -0500
committer-time 1704067200
committer-tz -0500
filename file.go
bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb 2 2 1
author Newer Author
author-time 1704067200
author-tz +0000
committer-time 1735689600
committer-tz +0000
filename file.go
`
	got, err := ParsePorcelain([]byte(output))
	if err != nil {
		t.Fatal(err)
	}
	if got.Author != "Newer Author" || got.Date != "2025-01-01T00:00:00Z" {
		t.Fatalf("unexpected blame: %#v", got)
	}
}

func TestEnrichSkipsFailures(t *testing.T) {
	files := []document.File{{
		Path: "file.go", Status: "modified",
		Hunks: []document.Hunk{{Lines: []document.HunkLine{{Type: "a", New: 4, Text: "new"}}}},
	}}
	Enrich(context.Background(), "/repo", files, failingRunner{})
	if files[0].Hunks[0].Blame != nil {
		t.Fatalf("failed blame should be omitted: %#v", files[0].Hunks[0].Blame)
	}
}

func TestEnrichPassesHardenedGitArguments(t *testing.T) {
	runner := &recordingRunner{output: []byte(`aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa 4 4 1
author Reviewer
author-time 1735689600
author-tz +0000
committer-time 1735689600
committer-tz +0000
filename file.go
`)}
	files := []document.File{{
		Path: "file.go", Status: "modified",
		Hunks: []document.Hunk{{Lines: []document.HunkLine{
			{Type: "c", New: 3, Text: "context"},
			{Type: "a", New: 4, Text: "new"},
		}}},
	}}
	Enrich(context.Background(), "/repo", files, runner)
	if files[0].Hunks[0].Blame == nil {
		t.Fatal("expected blame metadata")
	}
	joined := strings.Join(runner.args, " ")
	for _, expected := range []string{"color.ui=false", "--porcelain", "--no-textconv", "-L 3,4", "HEAD -- file.go"} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("missing %q in args: %s", expected, joined)
		}
	}
}

func TestEnrichUncommittedUsesHeadSideCoordinates(t *testing.T) {
	runner := &recordingRunner{output: []byte(`aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa 7 7 1
author Reviewer
author-time 1735689600
author-tz +0000
committer-time 1735689600
committer-tz +0000
filename file.go
`)}
	files := []document.File{{
		Path: "file.go", Status: "modified",
		Hunks: []document.Hunk{{Lines: []document.HunkLine{
			{Type: "a", New: 3, Text: "inserted"},
			{Type: "c", Old: 7, New: 8, Text: "existing"},
			{Type: "d", Old: 8, Text: "deleted"},
		}}},
	}}
	EnrichUncommitted(context.Background(), "/repo", files, runner)
	if joined := strings.Join(runner.args, " "); !strings.Contains(joined, "-L 7,8") {
		t.Fatalf("uncommitted blame did not use HEAD coordinates: %s", joined)
	}
}

type failingRunner struct{}

func (failingRunner) Run(context.Context, string, ...string) ([]byte, error) {
	return nil, errors.New("blame failed")
}

type recordingRunner struct {
	args   []string
	output []byte
}

func (r *recordingRunner) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
	r.args = append([]string(nil), args...)
	return r.output, nil
}
