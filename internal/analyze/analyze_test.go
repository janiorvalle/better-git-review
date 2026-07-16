package analyze

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/janiorvalle/better-git-review/internal/document"
)

type sequenceProvider struct {
	responses []string
	prompts   []string
}

func (p *sequenceProvider) Name() string { return "sequence" }
func (p *sequenceProvider) Detect() (bool, string) {
	return true, "test"
}
func (p *sequenceProvider) Complete(_ context.Context, prompt string) (string, error) {
	p.prompts = append(p.prompts, prompt)
	return p.responses[len(p.prompts)-1], nil
}

func TestRunRetriesOnceWithValidationErrors(t *testing.T) {
	provider := &sequenceProvider{responses: []string{
		`{"title":"bad","overview":"","mermaid":null,"cohorts":[]}`,
		`{"title":"ok","overview":"done","mermaid":null,"cohorts":[{"title":"Backend","layer":"backend","intent":"x","narrative":"y","files":[0],"fileSummaries":["changed"],"reviewNotes":[],"dependsOn":[]}]}`,
	}}
	analysis, err := Run(context.Background(), Options{
		Provider: provider,
		Source:   document.Source{Title: "test"},
		Files:    []document.File{{Path: "a.go", Status: "modified"}},
		StateDir: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if analysis.Title != "ok" || len(provider.prompts) != 2 {
		t.Fatalf("unexpected result or call count: %#v, %d", analysis, len(provider.prompts))
	}
	if !strings.Contains(provider.prompts[1], "cohorts must contain at least one item") {
		t.Fatalf("retry prompt did not contain exact validation error: %s", provider.prompts[1])
	}
}

func TestRunHardFailureWritesDebugOutput(t *testing.T) {
	stateDir := t.TempDir()
	provider := &sequenceProvider{responses: []string{"not json", "still not json"}}
	_, err := Run(context.Background(), Options{
		Provider: provider,
		Source:   document.Source{Title: "test"},
		Files:    []document.File{{Path: "a.go"}},
		StateDir: stateDir,
	})
	if err == nil || !strings.Contains(err.Error(), stateDir) {
		t.Fatalf("expected debug path in error, got %v", err)
	}
	entries, readErr := os.ReadDir(stateDir)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d debug files, want 1", len(entries))
	}
	data, readErr := os.ReadFile(stateDir + "/" + entries[0].Name())
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(data) != "still not json" {
		t.Fatalf("debug output = %q", data)
	}
}

type failingProvider struct {
	calls int
}

func (p *failingProvider) Name() string { return "failing" }
func (p *failingProvider) Detect() (bool, string) {
	return true, "test"
}
func (p *failingProvider) Complete(context.Context, string) (string, error) {
	p.calls++
	return "", errors.New("authentication failed")
}

func TestRunDoesNotRetryProviderExecutionFailure(t *testing.T) {
	provider := &failingProvider{}
	_, err := Run(context.Background(), Options{
		Provider: provider,
		Source:   document.Source{Title: "test"},
		Files:    []document.File{{Path: "a.go"}},
		StateDir: t.TempDir(),
	})
	if err == nil || !strings.Contains(err.Error(), "authentication failed") {
		t.Fatalf("unexpected error: %v", err)
	}
	if provider.calls != 1 {
		t.Fatalf("provider called %d times, want 1", provider.calls)
	}
}

type structuredSequenceProvider struct {
	calls int
}

func (p *structuredSequenceProvider) Name() string { return "structured" }
func (p *structuredSequenceProvider) Detect() (bool, string) {
	return true, "test"
}
func (p *structuredSequenceProvider) Complete(context.Context, string) (string, error) {
	panic("plain completion should not be used")
}
func (p *structuredSequenceProvider) CompleteStructured(_ context.Context, _ string, schema json.RawMessage) (json.RawMessage, error) {
	p.calls++
	if !json.Valid(schema) {
		panic("invalid schema")
	}
	return json.RawMessage(`{"title":"ok","overview":"done","mermaid":null,"cohorts":[{"title":"Backend","layer":"backend","intent":"x","narrative":"y","files":[0],"fileSummaries":["changed"],"reviewNotes":[],"dependsOn":[]}]}`), nil
}

func TestRunUsesStructuredProvider(t *testing.T) {
	provider := &structuredSequenceProvider{}
	_, err := Run(context.Background(), Options{
		Provider: provider,
		Source:   document.Source{Title: "test"},
		Files:    []document.File{{Path: "a.go"}},
		StateDir: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if provider.calls != 1 {
		t.Fatalf("calls = %d, want 1", provider.calls)
	}
}
