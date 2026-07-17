package provider

import (
	"context"
	"slices"
	"testing"

	"github.com/janiorvalle/better-git-review/internal/config"
)

func TestRegistrySelectsConfiguredAdapter(t *testing.T) {
	registry := NewRegistry(
		fakeAdapter{name: "first", available: false},
		fakeAdapter{name: "second", available: true},
	)
	selection, err := registry.Select(SelectOptions{
		Config: config.Config{
			Provider: "second",
			Providers: map[string]config.ProviderConfig{
				"second": {Model: "configured"},
			},
		},
		ModelOverride: "override",
	})
	if err != nil {
		t.Fatal(err)
	}
	if selection.Provider.Name() != "second" || selection.Model != "override" {
		t.Fatalf("unexpected selection: %#v", selection)
	}
	if names := registry.Names(); !slices.Equal(names, []string{"first", "second"}) {
		t.Fatalf("names = %#v", names)
	}
}

func TestRegistryAutoDetectionPreservesOrder(t *testing.T) {
	registry := NewRegistry(
		fakeAdapter{name: "first", available: false},
		fakeAdapter{name: "second", available: true},
		fakeAdapter{name: "third", available: true},
	)
	selection, err := registry.Select(SelectOptions{Config: config.Config{}})
	if err != nil {
		t.Fatal(err)
	}
	if selection.Provider.Name() != "second" {
		t.Fatalf("selected %q", selection.Provider.Name())
	}
}

type fakeAdapter struct {
	name      string
	available bool
}

func (a fakeAdapter) Name() string {
	return a.name
}

func (a fakeAdapter) New(opts AdapterOptions) (Provider, string, string, []string, error) {
	model := ChooseModel(opts.ModelOverride, opts.ConfiguredModel, "default")
	reasoning := ChooseReasoning(opts.ReasoningOverride, opts.ConfiguredReasoning, "")
	return fakeProvider{name: a.name, available: a.available}, model, reasoning, nil, nil
}

type fakeProvider struct {
	name      string
	available bool
}

func (p fakeProvider) Name() string {
	return p.name
}

func (p fakeProvider) Detect() (bool, string) {
	return p.available, "test"
}

func (p fakeProvider) Complete(context.Context, string) (string, error) {
	return "", nil
}
