package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/janiorvalle/better-git-review/internal/config"
)

type Provider interface {
	Name() string
	Detect() (available bool, detail string)
	Complete(ctx context.Context, prompt string) (string, error)
}

type StructuredProvider interface {
	Provider
	CompleteStructured(ctx context.Context, prompt string, schema json.RawMessage) (json.RawMessage, error)
}

type Selection struct {
	Provider Provider
	Model    string
}

type SelectOptions struct {
	Config        config.Config
	ModelOverride string
}

func Select(opts SelectOptions) (Selection, error) {
	if opts.Config.Provider != "" {
		return selectNamed(opts.Config.Provider, opts)
	}

	var probes []string
	for _, name := range []string{"claude-cli", "codex-cli", "openrouter"} {
		selection, err := selectNamed(name, opts)
		if err != nil {
			probes = append(probes, fmt.Sprintf("%s: %s", name, err))
			continue
		}
		available, detail := selection.Provider.Detect()
		probes = append(probes, fmt.Sprintf("%s: %s", name, detail))
		if available {
			return selection, nil
		}
	}
	return Selection{}, fmt.Errorf(
		"no analysis provider available; probed %s. Configure provider in ~/.config/better-git-review/config.toml or pass --provider",
		joinProbes(probes),
	)
}

func selectNamed(name string, opts SelectOptions) (Selection, error) {
	providerConfig := opts.Config.Providers[name]
	switch name {
	case "claude-cli":
		model := chooseModel(opts.ModelOverride, providerConfig.Model, "sonnet")
		return Selection{Provider: &ClaudeCLI{Model: model}, Model: model}, nil
	case "codex-cli":
		model := chooseModel(opts.ModelOverride, providerConfig.Model, "default")
		return Selection{Provider: &CodexCLI{Model: model}, Model: model}, nil
	case "openrouter":
		model := chooseModel(opts.ModelOverride, providerConfig.Model, "anthropic/claude-sonnet-4-5")
		keyEnv := defaultString(providerConfig.APIKeyEnv, "OPENROUTER_API_KEY")
		baseURL := defaultString(providerConfig.BaseURL, "https://openrouter.ai/api/v1")
		return Selection{
			Provider: &OpenRouter{
				Model:     model,
				APIKeyEnv: keyEnv,
				BaseURL:   baseURL,
				Getenv:    os.Getenv,
			},
			Model: model,
		}, nil
	case "mock":
		return Selection{Provider: &Mock{}, Model: chooseModel(opts.ModelOverride, providerConfig.Model, "deterministic")}, nil
	default:
		return Selection{}, fmt.Errorf("unknown provider %q (supported: claude-cli, codex-cli, openrouter, mock)", name)
	}
}

func chooseModel(override, configured, fallback string) string {
	if override != "" {
		return override
	}
	return defaultString(configured, fallback)
}

func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func joinProbes(probes []string) string {
	result := ""
	for i, probe := range probes {
		if i > 0 {
			result += "; "
		}
		result += probe
	}
	return result
}
