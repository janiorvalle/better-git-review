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

func (Adapter) New(opts provider.AdapterOptions) (provider.Provider, string, error) {
	model := provider.ChooseModel(opts.ModelOverride, opts.ConfiguredModel, "default")
	return &CLI{Model: model}, model, nil
}

type CLI struct {
	Model string
}

func (p *CLI) Name() string { return "codex-cli" }

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
	args = append(args, "-")
	if _, err := provider.RunCommand(ctx, isolatedDir, []byte(prompt), "codex", args...); err != nil {
		return "", err
	}
	output, err := os.ReadFile(outputPath)
	if err != nil {
		return "", fmt.Errorf("read codex last message: %w", err)
	}
	return string(output), nil
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
