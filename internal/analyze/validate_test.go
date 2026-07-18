package analyze

import (
	"slices"
	"strings"
	"testing"

	"github.com/janiorvalle/better-git-review/internal/document"
)

func validAnalysis() document.Analysis {
	return document.Analysis{Cohorts: []document.Cohort{
		{
			Title: "Backend", Layer: "backend", Files: []int{0},
			FileSummaries: []string{"backend"}, ReviewNotes: []string{}, DependsOn: []int{},
		},
		{
			Title: "Tests", Layer: "tests", Files: []int{1},
			FileSummaries: []string{"tests"}, ReviewNotes: []string{}, DependsOn: []int{0},
		},
	}}
}

func TestValidateFailures(t *testing.T) {
	analysis := validAnalysis()
	analysis.Cohorts[0].Layer = "banana"
	analysis.Cohorts[0].Files = []int{4}
	analysis.Cohorts[0].FileSummaries = nil
	analysis.Cohorts[1].DependsOn = []int{1}
	errors := Validate(analysis, 2)
	for _, expected := range []string{"allowed enum", "out-of-range", "parallel", "not an earlier", "not assigned"} {
		if !containsSubstring(errors, expected) {
			t.Fatalf("missing %q in %#v", expected, errors)
		}
	}
}

func TestValidateBeforeSeatbeltsRejectsIncompleteContent(t *testing.T) {
	analysis := document.Analysis{Cohorts: []document.Cohort{{
		Layer: "backend", Files: []int{0}, FileSummaries: []string{""},
	}}}
	errors := validateBeforeSeatbelts(analysis, 1)
	for _, expected := range []string{
		"title must not be empty",
		"overview must not be empty",
		"cohorts[0].title",
		"cohorts[0].intent",
		"cohorts[0].narrative",
		"cohorts[0].reviewNotes",
		"cohorts[0].dependsOn",
	} {
		if !containsSubstring(errors, expected) {
			t.Fatalf("missing %q in %#v", expected, errors)
		}
	}
}

func TestApplySeatbelts(t *testing.T) {
	analysis := document.Analysis{Cohorts: []document.Cohort{
		{
			Title: "Odd", Layer: "not-real", Files: []int{0, 0, 99},
			FileSummaries: []string{"first", "duplicate", "bad"}, DependsOn: []int{0, 9},
		},
		{
			Title: "Empty", Layer: "tests", Files: []int{99},
			FileSummaries: []string{"bad"},
		},
	}}
	got := ApplySeatbelts(analysis, make([]document.File, 3), false)
	if len(got.Cohorts) != 2 {
		t.Fatalf("got %d cohorts, want 2: %#v", len(got.Cohorts), got.Cohorts)
	}
	if got.Cohorts[0].Layer != "other" {
		t.Fatalf("bad layer was not normalized: %#v", got.Cohorts[0])
	}
	if !slices.Equal(got.Cohorts[0].Files, []int{0}) || !slices.Equal(got.Cohorts[0].FileSummaries, []string{"first"}) {
		t.Fatalf("duplicate/out-of-range files not removed: %#v", got.Cohorts[0])
	}
	if len(got.Cohorts[0].DependsOn) != 0 {
		t.Fatalf("invalid dependencies not removed: %#v", got.Cohorts[0].DependsOn)
	}
	catchAll := got.Cohorts[1]
	if catchAll.Title != "Other changes" || !slices.Equal(catchAll.Files, []int{1, 2}) ||
		len(catchAll.FileSummaries) != 2 {
		t.Fatalf("stray files not appended: %#v", catchAll)
	}
	if errors := Validate(got, 3); len(errors) > 0 {
		t.Fatalf("normalized analysis is invalid: %#v", errors)
	}
}

func TestApplySeatbeltsRemapsDependenciesAfterDroppingCohorts(t *testing.T) {
	analysis := document.Analysis{Cohorts: []document.Cohort{
		{Title: "Empty", Layer: "other", Files: []int{}, FileSummaries: []string{}},
		{
			Title: "Backend", Layer: "backend", Files: []int{0},
			FileSummaries: []string{"backend"}, DependsOn: []int{},
		},
		{
			Title: "Tests", Layer: "tests", Files: []int{1},
			FileSummaries: []string{"tests"}, DependsOn: []int{1},
		},
	}}
	got := ApplySeatbelts(analysis, make([]document.File, 2), false)
	if len(got.Cohorts) != 2 || !slices.Equal(got.Cohorts[1].DependsOn, []int{0}) {
		t.Fatalf("dependencies were not remapped: %#v", got.Cohorts)
	}
}

func TestApplySeatbeltsOrdersFilesAndParallelSummaries(t *testing.T) {
	files := []document.File{
		graphTestFile("src/a-consumer.ts", `import { buildMoney } from "./z-definition"`),
		graphTestFile("src/z-definition.ts", `export function buildMoney() { return 1 }`),
	}
	analysis := document.Analysis{Cohorts: []document.Cohort{{
		Title: "Backend", Layer: "backend", Files: []int{0, 1},
		FileSummaries: []string{"consumer", "definition"}, ReviewNotes: []string{}, DependsOn: []int{},
	}}}
	got := ApplySeatbelts(analysis, files, true)
	if !slices.Equal(got.Cohorts[0].Files, []int{1, 0}) ||
		!slices.Equal(got.Cohorts[0].FileSummaries, []string{"definition", "consumer"}) {
		t.Fatalf("ordered cohort = %#v", got.Cohorts[0])
	}
}

func TestValidateCompleteRequiresStubbedFiles(t *testing.T) {
	analysis := validAnalysis()
	analysis.Title = "Change"
	analysis.Overview = "Overview"
	for index := range analysis.Cohorts {
		analysis.Cohorts[index].Intent = "Intent"
		analysis.Cohorts[index].Narrative = "Narrative"
	}
	if errors := ValidateComplete(analysis, 2); !containsSubstring(errors, "stubbedFiles must be present") {
		t.Fatalf("missing required-array error: %#v", errors)
	}
	analysis.StubbedFiles = []int{}
	analysis.MechanicalFiles = []int{}
	analysis.FileKeySymbols = [][]string{{}, {}}
	analysis.StubbedCohorts = []int{}
	if errors := ValidateComplete(analysis, 2); len(errors) != 0 {
		t.Fatalf("empty provenance arrays should be valid: %#v", errors)
	}
	analysis.StubbedFiles = []int{0}
	analysis.MechanicalFiles = []int{0}
	if errors := ValidateComplete(analysis, 2); !containsSubstring(errors, "both stubbed and mechanical") {
		t.Fatalf("overlapping provenance should fail: %#v", errors)
	}
}

func containsSubstring(values []string, substring string) bool {
	for _, value := range values {
		if strings.Contains(value, substring) {
			return true
		}
	}
	return false
}
