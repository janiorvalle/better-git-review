package gitattr

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/janiorvalle/better-git-review/internal/document"
)

type fakeRunner struct {
	output []byte
	err    error
	ref    string
	paths  []string
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
	r.ref = ref
	r.paths = append([]string(nil), paths...)
	return r.output, r.err
}

func TestGeneratedParsesLinguistAttributeAndUsesReviewedRef(t *testing.T) {
	runner := &fakeRunner{output: []byte(
		"src/generated.go\x00linguist-generated\x00true\x00" +
			"src/normal.go\x00linguist-generated\x00unspecified\x00",
	)}
	files := []document.File{{Path: "src/generated.go"}, {Path: "src/normal.go"}}
	got := Generated(context.Background(), "/repo", "deadbeef", files, runner, nil)
	if !got[0] || got[1] || runner.ref != "deadbeef" || len(runner.paths) != 2 {
		t.Fatalf("generated = %#v, runner = %#v", got, runner)
	}
}

func TestGeneratedFailsOpen(t *testing.T) {
	logged := false
	got := Generated(
		context.Background(),
		"/repo",
		"deadbeef",
		[]document.File{{Path: "src/generated.go"}},
		&fakeRunner{err: errors.New("old git")},
		func(string, ...any) { logged = true },
	)
	if len(got) != 0 || !logged {
		t.Fatalf("failure did not stay review-worthy: %#v, logged=%t", got, logged)
	}
}
