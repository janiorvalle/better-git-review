package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/janiorvalle/better-git-review/internal/agentskill"
)

func TestInstallSkillInstallsForBothHarnessesByDefault(t *testing.T) {
	home := t.TempDir()
	var stdout bytes.Buffer
	err := Run(context.Background(), []string{"install-skill", "--home", home}, Environment{
		Stdout: &stdout,
		Stderr: &bytes.Buffer{},
		Getwd:  func() (string, error) { return home, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []agentskill.Target{agentskill.Claude, agentskill.Codex} {
		path, _ := agentskill.Path(home, target)
		data, readErr := os.ReadFile(path)
		if readErr != nil || !bytes.Equal(data, agentskill.Content) {
			t.Fatalf("%s: skill not installed correctly: %v", target, readErr)
		}
		if !strings.Contains(stdout.String(), path) {
			t.Fatalf("stdout does not name %s: %q", path, stdout.String())
		}
	}
}

func TestInstallSkillHonoursASingleHarnessFlag(t *testing.T) {
	home := t.TempDir()
	err := Run(context.Background(), []string{"install-skill", "--codex", "--home", home}, Environment{
		Stdout: &bytes.Buffer{},
		Stderr: &bytes.Buffer{},
		Getwd:  func() (string, error) { return home, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(filepath.Join(home, ".codex", "skills", "bgr", "SKILL.md")); statErr != nil {
		t.Fatalf("codex skill missing: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(home, ".claude", "skills", "bgr", "SKILL.md")); !os.IsNotExist(statErr) {
		t.Fatalf("claude skill should not be installed with --codex, got %v", statErr)
	}
}

func TestInstallSkillRejectsBothHarnessFlags(t *testing.T) {
	err := Run(context.Background(), []string{"install-skill", "--claude", "--codex", "--home", t.TempDir()}, Environment{
		Stdout: &bytes.Buffer{},
		Stderr: &bytes.Buffer{},
		Getwd:  func() (string, error) { return t.TempDir(), nil },
	})
	if err == nil || !strings.Contains(err.Error(), "not both") {
		t.Fatalf("expected a not-both error, got %v", err)
	}
}
