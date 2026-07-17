package agentskill

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed SKILL.md
var Content []byte

type Target string

const (
	Claude Target = "claude"
	Codex  Target = "codex"
)

func Path(home string, target Target) (string, error) {
	switch target {
	case Claude:
		return filepath.Join(home, ".claude", "skills", "bgr", "SKILL.md"), nil
	case Codex:
		return filepath.Join(home, ".codex", "skills", "bgr", "SKILL.md"), nil
	default:
		return "", fmt.Errorf("unknown skill target %q", target)
	}
}

func Install(home string, target Target) (string, error) {
	path, err := Path(home, target)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".skill-*.md")
	if err != nil {
		return "", err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		return "", err
	}
	if _, err := temp.Write(Content); err != nil {
		temp.Close()
		return "", err
	}
	if err := temp.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tempName, path); err != nil {
		return "", err
	}
	return path, nil
}
