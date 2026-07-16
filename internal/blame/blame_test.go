package blame

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/janiorvalle/better-git-review/internal/document"
)

func TestParsePorcelainUsesMostRecentAuthor(t *testing.T) {
	output := `aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa 1 1 1
author Older Author
author-time 1704067200
author-tz -0500
filename file.go
bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb 2 2 1
author Newer Author
author-time 1735689600
author-tz +0000
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
