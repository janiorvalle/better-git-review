package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/janiorvalle/better-git-review/internal/provider"
)

type Adapter struct{}

func (Adapter) Name() string {
	return "claude-cli"
}

func (Adapter) New(opts provider.AdapterOptions) (provider.Provider, string, string, []string, error) {
	timeoutSeconds := opts.ProviderExecTimeoutSeconds
	if timeoutSeconds <= 0 {
		timeoutSeconds = 600
	}
	return newWithEffortSupport(opts, supportsEffort(time.Duration(timeoutSeconds)*time.Second))
}

func newWithEffortSupport(opts provider.AdapterOptions, effortSupported bool) (provider.Provider, string, string, []string, error) {
	if opts.ProviderExecTimeoutSeconds <= 0 {
		opts.ProviderExecTimeoutSeconds = 600
	}
	model := provider.ChooseModel(opts.ModelOverride, opts.ConfiguredModel, "sonnet")
	reasoning := provider.ChooseReasoning(opts.ReasoningOverride, opts.ConfiguredReasoning, "")
	if err := provider.ValidateReasoning("claude-cli", reasoning, claudeReasoningLevels...); err != nil {
		return nil, "", "", nil, err
	}
	warnings := []string(nil)
	if reasoning != "" && !effortSupported {
		warnings = append(warnings, "installed claude-cli does not support --effort; continuing without applying reasoning")
		reasoning = ""
	}
	return &CLI{
		Model: model, Reasoning: reasoning, EffortSupported: effortSupported,
		ExecTimeout: time.Duration(opts.ProviderExecTimeoutSeconds) * time.Second,
	}, model, reasoning, warnings, nil
}

type CLI struct {
	Model           string
	Reasoning       string
	EffortSupported bool
	ExecTimeout     time.Duration
}

func (p *CLI) Name() string { return "claude-cli" }

func (p *CLI) AnalysisBudget(context.Context) int {
	switch p.Model {
	case "sonnet", "opus", "haiku":
		return 400_000
	default:
		return provider.DefaultAnalysisBudget
	}
}

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
	args := []string{
		"-p",
		"--safe-mode",
		"--tools", "",
		"--no-session-persistence",
		"--model", p.Model,
		"--output-format", "json",
	}
	if p.Reasoning != "" && p.EffortSupported {
		args = append(args, "--effort", p.Reasoning)
	}
	output, err := provider.RunCommandTimeout(ctx, p.ExecTimeout, isolatedDir, []byte(prompt), "claude", args...)
	if err != nil {
		return "", err
	}
	return ParseOutput(output)
}

func (p *CLI) Models(context.Context) ([]provider.ModelOption, error) {
	return []provider.ModelOption{
		{ID: "sonnet", Label: "Sonnet", Note: "recommended", Default: true},
		{ID: "opus", Label: "Opus", Note: "highest capability"},
		{ID: "haiku", Label: "Haiku", Note: "fastest"},
	}, nil
}

func (p *CLI) ReasoningLevels() []string {
	if !p.EffortSupported {
		return nil
	}
	return append([]string(nil), claudeReasoningLevels...)
}

var claudeReasoningLevels = []string{"low", "medium", "high", "xhigh", "max"}

func supportsEffort(timeout time.Duration) bool {
	output, err := provider.RunCommandTimeout(context.Background(), timeout, "", nil, "claude", "--help")
	return err == nil && strings.Contains(string(output), "--effort")
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
