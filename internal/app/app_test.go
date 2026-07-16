package app

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/janiorvalle/better-git-review/internal/document"
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
