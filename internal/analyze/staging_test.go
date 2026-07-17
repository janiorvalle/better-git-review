package analyze

import (
	"fmt"
	"strings"
	"testing"

	"github.com/janiorvalle/better-git-review/internal/document"
)

func TestDecideStagingBoundariesAndOverride(t *testing.T) {
	files := []document.File{{
		Path: "main.go",
		Hunks: []document.Hunk{{
			Lines: []document.HunkLine{{Type: "a", Text: strings.Repeat("x", 2_000)}},
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

func TestTinyStageBudgetOverrideUsesDerivedPromptMinimum(t *testing.T) {
	files := []document.File{{
		Path:  "main.go",
		Hunks: []document.Hunk{{Lines: []document.HunkLine{{Type: "a", Text: "changed"}}}},
	}}
	decision, err := DecideStaging(files, func(string) string { return "1" })
	if err != nil {
		t.Fatal(err)
	}
	if !decision.Staged || decision.Budget != minimumStagedBudget(files) {
		t.Fatalf("decision = %#v, minimum = %d", decision, minimumStagedBudget(files))
	}
}

func TestProviderBudgetBelowStagedPromptMinimumFailsClosed(t *testing.T) {
	files := make([]document.File, CohortMaxFiles+1)
	for index := range files {
		files[index] = document.File{Path: fmt.Sprintf("src/%03d.go", index)}
	}
	minimum := minimumStagedBudget(files)
	_, err := DecideStaging(files, func(string) string { return "" }, minimum-1)
	if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("minimum %d", minimum)) {
		t.Fatalf("error = %v, want derived minimum", err)
	}
}

func TestStageBudgetRejectsInvalidOverride(t *testing.T) {
	for _, value := range []string{"nope", "0", "-1"} {
		if _, err := StageBudget(func(string) string { return value }); err == nil {
			t.Fatalf("expected %q to fail", value)
		}
	}
}

func TestDecideStagingUsesProviderBudgetWithoutOverride(t *testing.T) {
	files := []document.File{{
		Path:  "main.go",
		Hunks: []document.Hunk{{Lines: []document.HunkLine{{Type: "a", Text: "changed"}}}},
	}}
	size := AnalysisInputBytes(files)
	decision, err := DecideStaging(files, func(string) string { return "" }, size+1)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Staged || decision.Budget != size+1 {
		t.Fatalf("decision = %#v", decision)
	}
}

func TestDecideStagingCapsSinglePassFileStructures(t *testing.T) {
	files := make([]document.File, CohortMaxFiles+1)
	for index := range files {
		files[index] = document.File{Path: fmt.Sprintf("src/%03d.go", index)}
	}
	decision, err := DecideStaging(files, func(string) string { return "" }, 10_000_000)
	if err != nil {
		t.Fatal(err)
	}
	if !decision.Staged {
		t.Fatal("151 tiny files must use staged analysis")
	}
	decision, err = DecideStaging(files[:CohortMaxFiles], func(string) string { return "" }, 10_000_000)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Staged {
		t.Fatal("150 tiny files should preserve single-pass analysis")
	}
}
