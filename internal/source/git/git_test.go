package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/janiorvalle/better-git-review/internal/source"
)

func TestDetectBaseSkipsDanglingOriginHead(t *testing.T) {
	repo := t.TempDir()
	runGitTest(t, repo, "init", "-b", "main")
	runGitTest(t, repo, "config", "user.email", "source@example.com")
	runGitTest(t, repo, "config", "user.name", "Source Test")
	if err := os.WriteFile(filepath.Join(repo, "file.txt"), []byte("base\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, repo, "add", "file.txt")
	runGitTest(t, repo, "commit", "-m", "base")
	runGitTest(t, repo, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/missing")

	base, err := DetectBase(context.Background(), repo, nil)
	if err != nil {
		t.Fatal(err)
	}
	if base != "main" {
		t.Fatalf("base = %q, want main", base)
	}
}

func TestCollectSupportsHeadAndCommitRanges(t *testing.T) {
	repo := t.TempDir()
	runGitTest(t, repo, "init", "-b", "main")
	runGitTest(t, repo, "config", "user.email", "source@example.com")
	runGitTest(t, repo, "config", "user.name", "Source Test")
	writeGitFile(t, repo, "file.txt", "base\n")
	runGitTest(t, repo, "add", "file.txt")
	runGitTest(t, repo, "commit", "-m", "base")
	runGitTest(t, repo, "switch", "-c", "one")
	writeGitFile(t, repo, "one.txt", "one\n")
	runGitTest(t, repo, "add", "one.txt")
	runGitTest(t, repo, "commit", "-m", "one")
	oneSHA := strings.TrimSpace(runGitOutput(t, repo, "rev-parse", "HEAD"))
	runGitTest(t, repo, "switch", "main")
	runGitTest(t, repo, "switch", "-c", "two")
	writeGitFile(t, repo, "two.txt", "two\n")
	runGitTest(t, repo, "add", "two.txt")
	runGitTest(t, repo, "commit", "-m", "two")

	byHead, err := (Source{}).Collect(context.Background(), source.Options{
		RepoDir: repo, Base: "main", Head: "one", Logf: func(string, ...any) {},
	})
	if err != nil {
		t.Fatal(err)
	}
	if byHead.Source.Range != "main...one" || !strings.Contains(string(byHead.Diff), "one.txt") || strings.Contains(string(byHead.Diff), "two.txt") {
		t.Fatalf("unexpected head result: %#v\n%s", byHead.Source, byHead.Diff)
	}

	byCommit, err := (Source{}).Collect(context.Background(), source.Options{
		RepoDir: repo, Commit: oneSHA, Logf: func(string, ...any) {},
	})
	if err != nil {
		t.Fatal(err)
	}
	if byCommit.HeadRef != oneSHA || !strings.Contains(string(byCommit.Diff), "one.txt") {
		t.Fatalf("unexpected commit result: %#v\n%s", byCommit, byCommit.Diff)
	}
}

func TestCollectSupportsRootCommit(t *testing.T) {
	repo := t.TempDir()
	runGitTest(t, repo, "init", "-b", "main")
	runGitTest(t, repo, "config", "user.email", "source@example.com")
	runGitTest(t, repo, "config", "user.name", "Source Test")
	writeGitFile(t, repo, "root.txt", "root\n")
	runGitTest(t, repo, "add", "root.txt")
	runGitTest(t, repo, "commit", "-m", "root")
	rootSHA := strings.TrimSpace(runGitOutput(t, repo, "rev-parse", "HEAD"))

	result, err := (Source{}).Collect(context.Background(), source.Options{
		RepoDir: repo, Commit: rootSHA, Logf: func(string, ...any) {},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.BaseRef != "" || !strings.Contains(result.Source.Range, "root commit") || !strings.Contains(string(result.Diff), "root.txt") {
		t.Fatalf("unexpected root result: %#v\n%s", result, result.Diff)
	}
}

func writeGitFile(t *testing.T, repo, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func runGitOutput(t *testing.T, repo string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = repo
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, output)
	}
	return string(output)
}

func runGitTest(t *testing.T, repo string, args ...string) {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = repo
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, output)
	}
}
