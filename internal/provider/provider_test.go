package provider

import (
	"testing"

	"github.com/janiorvalle/better-git-review/internal/config"
)

func TestOpenRouterDefaultModel(t *testing.T) {
	selection, err := selectNamed("openrouter", SelectOptions{Config: config.Config{}})
	if err != nil {
		t.Fatal(err)
	}
	if selection.Model != "anthropic/claude-sonnet-4.5" {
		t.Fatalf("model = %q", selection.Model)
	}
}
