package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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

func runGitTest(t *testing.T, repo string, args ...string) {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = repo
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, output)
	}
}
