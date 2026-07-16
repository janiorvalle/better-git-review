package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

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

type Adapter interface {
	Name() string
	New(AdapterOptions) (Provider, string, error)
}

type AdapterOptions struct {
	ModelOverride   string
	ConfiguredModel string
	APIKeyEnv       string
	BaseURL         string
	Getenv          func(string) string
}

type Registry struct {
	adapters []Adapter
}

func NewRegistry(adapters ...Adapter) Registry {
	return Registry{adapters: append([]Adapter(nil), adapters...)}
}

type SelectOptions struct {
	Config        config.Config
	ModelOverride string
	Getenv        func(string) string
}

func (r Registry) Select(opts SelectOptions) (Selection, error) {
	if opts.Getenv == nil {
		opts.Getenv = os.Getenv
	}
	if opts.Config.Provider != "" {
		return r.selectNamed(opts.Config.Provider, opts)
	}

	var probes []string
	for _, adapter := range r.adapters {
		selection, err := r.selectNamed(adapter.Name(), opts)
		if err != nil {
			probes = append(probes, fmt.Sprintf("%s: %s", adapter.Name(), err))
			continue
		}
		available, detail := selection.Provider.Detect()
		probes = append(probes, fmt.Sprintf("%s: %s", adapter.Name(), detail))
		if available {
			return selection, nil
		}
	}
	return Selection{}, fmt.Errorf(
		"no analysis provider available; probed %s. Configure provider in ~/.config/better-git-review/config.toml or pass --provider",
		joinProbes(probes),
	)
}

func (r Registry) selectNamed(name string, opts SelectOptions) (Selection, error) {
	providerConfig := opts.Config.Providers[name]
	for _, adapter := range r.adapters {
		if adapter.Name() != name {
			continue
		}
		selected, model, err := adapter.New(AdapterOptions{
			ModelOverride:   opts.ModelOverride,
			ConfiguredModel: providerConfig.Model,
			APIKeyEnv:       providerConfig.APIKeyEnv,
			BaseURL:         providerConfig.BaseURL,
			Getenv:          opts.Getenv,
		})
		if err != nil {
			return Selection{}, err
		}
		return Selection{Provider: selected, Model: model}, nil
	}
	return Selection{}, fmt.Errorf("unknown provider %q (supported: %s)", name, strings.Join(r.Names(), ", "))
}

func ChooseModel(override, configured, fallback string) string {
	if override != "" {
		return override
	}
	return DefaultString(configured, fallback)
}

func DefaultString(value, fallback string) string {
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

func (r Registry) Names() []string {
	names := make([]string, 0, len(r.adapters))
	for _, adapter := range r.adapters {
		names = append(names, adapter.Name())
	}
	return names
}
