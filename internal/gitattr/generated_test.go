package gitattr

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/janiorvalle/better-git-review/internal/document"
)

type fakeRunner struct {
	responses []fakeResponse
	calls     []fakeCall
}

type fakeResponse struct {
	output []byte
	err    error
}

type fakeCall struct {
	ref   string
	paths []string
}

func TestExecRunnerReadsAttributesFromReviewedCommit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	repo := t.TempDir()
	runGit := func(args ...string) {
		t.Helper()
		command := exec.Command("git", args...)
		command.Dir = repo
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, output)
		}
	}
	runGit("init", "-q")
	runGit("config", "user.email", "test@example.com")
	runGit("config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(repo, ".gitattributes"),
		[]byte("*.gen linguist-generated=true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "asset.gen"), []byte("generated\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit("add", ".")
	runGit("commit", "-qm", "attributes")
	refBytes, err := exec.Command("git", "-C", repo, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	ref := string(refBytes[:len(refBytes)-1])
	if err := os.WriteFile(filepath.Join(repo, ".gitattributes"),
		[]byte("*.gen -linguist-generated\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	raw, err := (ExecRunner{}).Check(context.Background(), repo, ref, []string{"asset.gen"})
	if err != nil {
		if strings.Contains(err.Error(), "unknown option") && strings.Contains(err.Error(), "source") {
			t.Skip("installed git does not support check-attr --source")
		}
		t.Fatalf("git check-attr failed: %v", err)
	}
	if len(raw) == 0 {
		t.Fatal("git check-attr returned no records")
	}
	got := Generated(context.Background(), repo, ref,
		[]document.File{{Path: "asset.gen"}}, ExecRunner{}, nil)
	if !got[0] {
		t.Fatalf("reviewed-commit attribute was not detected: %#v", got)
	}
}

func (r *fakeRunner) Check(_ context.Context, _ string, ref string, paths []string) ([]byte, error) {
	r.calls = append(r.calls, fakeCall{ref: ref, paths: append([]string(nil), paths...)})
	response := r.responses[len(r.calls)-1]
	return response.output, response.err
}

func TestGeneratedParsesLinguistAttributeAndUsesReviewedRef(t *testing.T) {
	runner := &fakeRunner{responses: []fakeResponse{{output: []byte(
		"src/generated.go\x00linguist-generated\x00true\x00" +
			"src/normal.go\x00linguist-generated\x00unspecified\x00",
	)}}}
	files := []document.File{{Path: "src/generated.go"}, {Path: "src/normal.go"}}
	got := Generated(context.Background(), "/repo", "deadbeef", files, runner, nil)
	if !got[0] || got[1] || len(runner.calls) != 1 || runner.calls[0].ref != "deadbeef" ||
		len(runner.calls[0].paths) != 2 {
		t.Fatalf("generated = %#v, runner = %#v", got, runner)
	}
}

func TestGeneratedFallsBackToWorktreeAttributes(t *testing.T) {
	runner := &fakeRunner{responses: []fakeResponse{
		{err: errors.New("unknown option: source")},
		{output: []byte("src/generated.go\x00linguist-generated\x00true\x00")},
	}}
	var logs []string
	got := Generated(
		context.Background(),
		"/repo",
		"deadbeef",
		[]document.File{{Path: "src/generated.go"}},
		runner,
		func(format string, args ...any) { logs = append(logs, fmt.Sprintf(format, args...)) },
	)
	if !got[0] {
		t.Fatalf("worktree attribute was not detected: %#v", got)
	}
	if len(runner.calls) != 2 || runner.calls[0].ref != "deadbeef" || runner.calls[1].ref != "" {
		t.Fatalf("fallback calls = %#v", runner.calls)
	}
	if !slices.Equal(logs, []string{"could not read attributes from the reviewed commit; using worktree attributes"}) {
		t.Fatalf("fallback logs = %#v", logs)
	}
}

func TestGeneratedFailsOpen(t *testing.T) {
	runner := &fakeRunner{responses: []fakeResponse{
		{err: errors.New("reviewed ref failed")},
		{err: errors.New("worktree failed")},
	}}
	var logs []string
	got := Generated(
		context.Background(),
		"/repo",
		"deadbeef",
		[]document.File{{Path: "src/generated.go"}},
		runner,
		func(format string, args ...any) { logs = append(logs, fmt.Sprintf(format, args...)) },
	)
	if len(got) != 0 || len(runner.calls) != 2 || len(logs) != 2 ||
		!strings.Contains(logs[1], "keeping files review-worthy") {
		t.Fatalf("failure did not stay review-worthy: generated=%#v calls=%#v logs=%#v", got, runner.calls, logs)
	}
}

func TestFirstLineDiagnosticIsConcise(t *testing.T) {
	diagnostic := firstLineDiagnostic(strings.Repeat("é", 250) + "\nusage: git check-attr [options]")
	if len(diagnostic) > 200 || strings.Contains(diagnostic, "usage:") || strings.Contains(diagnostic, "\n") {
		t.Fatalf("diagnostic = %q (%d chars)", diagnostic, len(diagnostic))
	}
	if _, err := strconv.Unquote(diagnostic); err != nil {
		t.Fatalf("diagnostic is not valid quoted text: %q: %v", diagnostic, err)
	}
}
