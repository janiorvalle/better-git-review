package codex

import (
	"slices"
	"testing"

	"github.com/janiorvalle/better-git-review/internal/provider"
)

func TestAdapterDefaultsAndReasoningValidation(t *testing.T) {
	selected, model, reasoning, _, err := (Adapter{}).New(provider.AdapterOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if model != "gpt-5.6-luna" || reasoning != "low" {
		t.Fatalf("defaults = %q/%q", model, reasoning)
	}
	cli := selected.(*CLI)
	args := cli.args("workspace", "output")
	if !slices.Contains(args, `model_reasoning_effort="low"`) {
		t.Fatalf("reasoning config missing: %#v", args)
	}
	if _, _, _, _, err := (Adapter{}).New(provider.AdapterOptions{ReasoningOverride: "extreme"}); err == nil {
		t.Fatal("invalid reasoning should fail")
	}
	if _, _, _, _, err := (Adapter{}).New(provider.AdapterOptions{ReasoningOverride: "minimal"}); err == nil {
		t.Fatal("live Luna catalog does not support minimal reasoning")
	}
}
