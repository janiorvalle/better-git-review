package app

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/janiorvalle/better-git-review/internal/document"
	"github.com/janiorvalle/better-git-review/internal/source"
)

func TestParseArgsAllowsPRBeforeFlags(t *testing.T) {
	opts, err := parseArgs([]string{"123", "--provider", "mock", "--out", "result.json"}, Environment{
		Stderr: &bytes.Buffer{},
		Getwd:  func() (string, error) { return t.TempDir(), nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if opts.PR != "123" || opts.Provider != "mock" || opts.Out != "result.json" {
		t.Fatalf("unexpected options: %#v", opts)
	}
	if opts.Format != "html" {
		t.Fatalf("default format = %q, want html", opts.Format)
	}
}

func TestParseArgsRejectsConflictingSources(t *testing.T) {
	tests := [][]string{
		{"123", "--base", "main"},
		{"--diff", "change.patch", "--base", "main"},
		{"123", "--diff", "change.patch"},
		{"123", "--dirty"},
		{"--diff", "change.patch", "--dirty"},
		{"--base", "main", "--dirty"},
	}
	for _, args := range tests {
		if _, err := parseArgs(args, Environment{
			Stderr: &bytes.Buffer{},
			Getwd:  func() (string, error) { return t.TempDir(), nil },
		}); err == nil {
			t.Fatalf("expected conflicting args to fail: %#v", args)
		}
	}
}

func TestBrowserCommandDispatch(t *testing.T) {
	tests := []struct {
		goos string
		name string
		args []string
		ok   bool
	}{
		{goos: "darwin", name: "open", args: []string{"review.html"}, ok: true},
		{goos: "linux", name: "xdg-open", args: []string{"review.html"}, ok: true},
		{goos: "windows", name: "cmd", args: []string{"/c", "start", "", "review.html"}, ok: true},
		{goos: "plan9", ok: false},
	}
	for _, test := range tests {
		t.Run(test.goos, func(t *testing.T) {
			name, args, ok := browserCommand(test.goos, "review.html")
			if name != test.name || !slices.Equal(args, test.args) || ok != test.ok {
				t.Fatalf("got %q %#v %v", name, args, ok)
			}
		})
	}
}

func TestParseArgsAcceptsViewerFlags(t *testing.T) {
	opts, err := parseArgs([]string{"--format", "json", "--open"}, Environment{
		Stderr: &bytes.Buffer{},
		Getwd:  func() (string, error) { return t.TempDir(), nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if opts.Format != "json" || !opts.Open {
		t.Fatalf("unexpected viewer options: %#v", opts)
	}
}

func TestParseArgsRejectsUnknownFormat(t *testing.T) {
	if _, err := parseArgs([]string{"--format", "pdf"}, Environment{
		Stderr: &bytes.Buffer{},
		Getwd:  func() (string, error) { return t.TempDir(), nil },
	}); err == nil {
		t.Fatal("expected unknown format to fail")
	}
}

func TestRunVersionDoesNotRequireARepositoryOrProvider(t *testing.T) {
	var output bytes.Buffer
	if err := Run(context.Background(), []string{"--version"}, Environment{
		Stdout: &output,
		Stderr: &bytes.Buffer{},
		Getwd:  func() (string, error) { return t.TempDir(), nil },
	}); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(output.String()) != document.Generator() {
		t.Fatalf("version output = %q", output.String())
	}
}

func TestParseStageRejectsEmptyAndParsesFiles(t *testing.T) {
	if _, err := parseStage(source.Result{}, func(string, ...any) {}); err == nil {
		t.Fatal("empty diff should fail")
	}
	files, err := parseStage(source.Result{Diff: []byte(
		"diff --git a/a.go b/a.go\n" +
			"--- a/a.go\n" +
			"+++ b/a.go\n" +
			"@@ -1 +1 @@\n" +
			"-package old\n" +
			"+package current\n",
	)}, func(string, ...any) {})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Path != "a.go" {
		t.Fatalf("unexpected files: %#v", files)
	}
}

func TestRenderStageSupportsJSONAndHTML(t *testing.T) {
	value := document.Document{
		SchemaVersion: document.SchemaVersion,
		Source:        document.Source{Title: "Change", Name: "change"},
		Files:         []document.File{{Path: "a.go"}},
		Analysis: document.Analysis{
			Title: "Change", Overview: "Overview", StubbedFiles: []int{},
			Cohorts: []document.Cohort{{
				Title: "Backend", Layer: "backend", Intent: "Intent", Narrative: "Narrative",
				Files: []int{0}, FileSummaries: []string{"Summary"},
				ReviewNotes: []string{}, DependsOn: []int{},
			}},
		},
	}
	jsonOutput, err := renderStage("json", value)
	if err != nil {
		t.Fatal(err)
	}
	var decoded document.Document
	if err := json.Unmarshal(jsonOutput, &decoded); err != nil {
		t.Fatalf("invalid JSON output: %v", err)
	}
	htmlOutput, err := renderStage("html", value)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(htmlOutput), "<!doctype html>") {
		t.Fatal("HTML stage did not render a document")
	}
}

func TestOutputPathUsesSourceName(t *testing.T) {
	path, err := outputPath(options{Format: "html"}, "branch")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "walkthrough-branch.html" {
		t.Fatalf("path = %q", path)
	}
}
