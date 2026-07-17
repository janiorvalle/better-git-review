package codex

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/janiorvalle/better-git-review/internal/provider"
)

type Adapter struct{}

func (Adapter) Name() string {
	return "codex-cli"
}

func (Adapter) New(opts provider.AdapterOptions) (provider.Provider, string, string, []string, error) {
	model := provider.ChooseModel(opts.ModelOverride, opts.ConfiguredModel, "gpt-5.6-luna")
	reasoning := provider.ChooseReasoning(opts.ReasoningOverride, opts.ConfiguredReasoning, "low")
	if err := provider.ValidateReasoning("codex-cli", reasoning, codexReasoningLevels(model)...); err != nil {
		return nil, "", "", nil, err
	}
	return &CLI{Model: model, Reasoning: reasoning}, model, reasoning, nil, nil
}

type CLI struct {
	Model     string
	Reasoning string
}

func (p *CLI) Name() string { return "codex-cli" }

func (p *CLI) AnalysisBudget(context.Context) int {
	switch p.Model {
	case "gpt-5.6-luna", "gpt-5.6-terra", "gpt-5.6-sol":
		return 400_000
	default:
		return provider.DefaultAnalysisBudget
	}
}

func (p *CLI) Models(context.Context) ([]provider.ModelOption, error) {
	return []provider.ModelOption{
		{ID: "gpt-5.6-luna", Label: "Luna", Note: "recommended", Default: true},
		{ID: "gpt-5.6-terra", Label: "Terra", Note: "balanced"},
		{ID: "gpt-5.6-sol", Label: "Sol", Note: "frontier"},
	}, nil
}

func (p *CLI) ReasoningLevels() []string {
	return codexReasoningLevels(p.Model)
}

func (p *CLI) SetCatalogModel(model string) { p.Model = model }

func codexReasoningLevels(model string) []string {
	levels := []string{"low", "medium", "high", "xhigh", "max"}
	if model == "gpt-5.6-terra" || model == "gpt-5.6-sol" {
		levels = append(levels, "ultra")
	}
	return levels
}

func (p *CLI) Detect() (bool, string) {
	path, err := exec.LookPath("codex")
	if err != nil {
		return false, "codex executable not found"
	}
	return true, "found " + path
}

func (p *CLI) Complete(ctx context.Context, prompt string) (string, error) {
	isolatedDir, err := os.MkdirTemp("", "better-git-review-codex-workspace-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(isolatedDir)
	temp, err := os.CreateTemp("", "better-git-review-codex-*.txt")
	if err != nil {
		return "", err
	}
	outputPath := temp.Name()
	if err := temp.Close(); err != nil {
		return "", err
	}
	defer os.Remove(outputPath)

	args := p.args(isolatedDir, outputPath)
	if _, err := provider.RunCommand(ctx, isolatedDir, []byte(prompt), "codex", args...); err != nil {
		return "", err
	}
	output, err := os.ReadFile(outputPath)
	if err != nil {
		return "", fmt.Errorf("read codex last message: %w", err)
	}
	return string(output), nil
}

func (p *CLI) args(isolatedDir, outputPath string) []string {
	args := []string{
		"exec",
		"--ephemeral",
		"--ignore-user-config",
		"--ignore-rules",
	}
	for _, feature := range disabledFeatures {
		args = append(args, "--disable", feature)
	}
	args = append(args,
		"--config", `web_search="disabled"`,
		"--config", "tools_view_image=false",
		"--skip-git-repo-check",
		"--sandbox", "read-only",
		"--output-last-message", outputPath,
		"-C", isolatedDir,
	)
	if p.Model != "" && p.Model != "default" {
		args = append(args, "--model", p.Model)
	}
	if p.Reasoning != "" {
		args = append(args, "--config", fmt.Sprintf("model_reasoning_effort=%q", p.Reasoning))
	}
	args = append(args, "-")
	return args
}

var disabledFeatures = []string{
	"apps",
	"auth_elicitation",
	"browser_use",
	"browser_use_external",
	"browser_use_full_cdp_access",
	"code_mode",
	"code_mode_host",
	"computer_use",
	"enable_mcp_apps",
	"goals",
	"hooks",
	"image_generation",
	"in_app_browser",
	"multi_agent",
	"network_proxy",
	"plugin_sharing",
	"plugins",
	"remote_plugin",
	"request_permissions_tool",
	"shell_snapshot",
	"shell_tool",
	"skill_mcp_dependency_install",
	"standalone_web_search",
	"tool_call_mcp_elicitation",
	"tool_suggest",
	"unified_exec",
	"workspace_dependencies",
}
