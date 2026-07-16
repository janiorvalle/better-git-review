package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
)

type ClaudeCLI struct {
	Model string
}

func (p *ClaudeCLI) Name() string { return "claude-cli" }

func (p *ClaudeCLI) Detect() (bool, string) {
	path, err := exec.LookPath("claude")
	if err != nil {
		return false, "claude executable not found"
	}
	return true, "found " + path
}

func (p *ClaudeCLI) Complete(ctx context.Context, prompt string) (string, error) {
	isolatedDir, err := os.MkdirTemp("", "better-git-review-claude-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(isolatedDir)
	output, err := runCommand(ctx, isolatedDir, []byte(prompt), "claude",
		"-p",
		"--safe-mode",
		"--tools", "",
		"--no-session-persistence",
		"--model", p.Model,
		"--output-format", "json",
	)
	if err != nil {
		return "", err
	}
	return ParseClaudeOutput(output)
}

func ParseClaudeOutput(output []byte) (string, error) {
	var decoded any
	if err := json.Unmarshal(output, &decoded); err != nil {
		return string(output), nil
	}
	switch value := decoded.(type) {
	case string:
		return value, nil
	case map[string]any:
		if result, ok := value["result"].(string); ok {
			if isError, _ := value["is_error"].(bool); isError {
				return "", fmt.Errorf("claude returned an error: %s", safeDiagnostic(result, 300))
			}
			return result, nil
		}
	case []any:
		for _, event := range value {
			object, ok := event.(map[string]any)
			if !ok || object["type"] != "result" {
				continue
			}
			result, _ := object["result"].(string)
			if isError, _ := object["is_error"].(bool); isError {
				return "", fmt.Errorf("claude returned an error: %s", safeDiagnostic(result, 300))
			}
			if result != "" {
				return result, nil
			}
		}
	}
	return string(output), nil
}
