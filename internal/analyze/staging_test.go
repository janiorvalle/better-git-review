package analyze

import (
	"fmt"
	"testing"

	"github.com/janiorvalle/better-git-review/internal/document"
)

func TestDecideStagingBoundariesAndOverride(t *testing.T) {
	files := []document.File{{
		Path: "main.go",
		Hunks: []document.Hunk{{
			Lines: []document.HunkLine{{Type: "a", Text: "changed"}},
		}},
	}}
	size := AnalysisInputBytes(files)
	tests := []struct {
		name   string
		budget int
		staged bool
	}{
		{name: "under", budget: size + 1, staged: false},
		{name: "at", budget: size, staged: false},
		{name: "over", budget: size - 1, staged: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decision, err := DecideStaging(files, func(name string) string {
				if name != stageBudgetEnv {
					t.Fatalf("unexpected env key %q", name)
				}
				return fmt.Sprint(test.budget)
			})
			if err != nil {
				t.Fatal(err)
			}
			if decision.Staged != test.staged || decision.Budget != test.budget || decision.InputBytes != size {
				t.Fatalf("decision = %#v", decision)
			}
		})
	}
}

func TestStageBudgetRejectsInvalidOverride(t *testing.T) {
	for _, value := range []string{"nope", "0", "-1"} {
		if _, err := StageBudget(func(string) string { return value }); err == nil {
			t.Fatalf("expected %q to fail", value)
		}
	}
}
