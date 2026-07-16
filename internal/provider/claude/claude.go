package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"

	"github.com/janiorvalle/better-git-review/internal/provider"
)

type Adapter struct{}

func (Adapter) Name() string {
	return "claude-cli"
}

func (Adapter) New(opts provider.AdapterOptions) (provider.Provider, string, error) {
	model := provider.ChooseModel(opts.ModelOverride, opts.ConfiguredModel, "sonnet")
	return &CLI{Model: model}, model, nil
}

type CLI struct {
	Model string
}

func (p *CLI) Name() string { return "claude-cli" }

func (p *CLI) Detect() (bool, string) {
	path, err := exec.LookPath("claude")
	if err != nil {
		return false, "claude executable not found"
	}
	return true, "found " + path
}

func (p *CLI) Complete(ctx context.Context, prompt string) (string, error) {
	isolatedDir, err := os.MkdirTemp("", "better-git-review-claude-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(isolatedDir)
	output, err := provider.RunCommand(ctx, isolatedDir, []byte(prompt), "claude",
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
	return ParseOutput(output)
}

func ParseOutput(output []byte) (string, error) {
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
				return "", fmt.Errorf("claude returned an error: %s", provider.SafeDiagnostic(result, 300))
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
				return "", fmt.Errorf("claude returned an error: %s", provider.SafeDiagnostic(result, 300))
			}
			if result != "" {
				return result, nil
			}
		}
	}
	return string(output), nil
}
