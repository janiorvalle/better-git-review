package codex

import (
	"context"
	"slices"
	"testing"

	"github.com/janiorvalle/better-git-review/internal/provider"
)

func TestAnalysisBudgetUsesOnlyApprovedModels(t *testing.T) {
	if got := (&CLI{Model: "gpt-5.6-luna"}).AnalysisBudget(context.Background()); got != 400_000 {
		t.Fatalf("luna budget = %d", got)
	}
	if got := (&CLI{Model: "future-model"}).AnalysisBudget(context.Background()); got != provider.DefaultAnalysisBudget {
		t.Fatalf("unknown model budget = %d", got)
	}
}

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
