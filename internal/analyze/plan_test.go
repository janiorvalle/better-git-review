package analyze

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/janiorvalle/better-git-review/internal/document"
)

func TestTriageOnlyDropsProvableMechanicalFiles(t *testing.T) {
	files := []document.File{
		{Path: "renamed.go", Status: "renamed", Similarity: 100},
		{Path: "edited-rename.go", Status: "renamed", Similarity: 99},
		{Path: "forged-rename.go", Status: "renamed", Similarity: 100, Additions: 1,
			Hunks: []document.Hunk{{Lines: []document.HunkLine{{Type: "a", Text: "real change"}}}}},
		{Path: "mode-rename.go", Status: "renamed", Similarity: 100, ModeChanged: true},
		{Path: "generated.go"},
		{Path: "image.png", Binary: true},
		{Path: "vendor/code.go"},
		{Path: "api.pb.go"},
		{Path: "package-lock.json"},
		{Path: "app.min.js"},
		{Path: "spaces.go", Hunks: []document.Hunk{{Lines: []document.HunkLine{
			{Type: "d", Text: "return value"}, {Type: "a", Text: "return   value"},
		}}}},
	}
	got := Triage(files, map[int]bool{4: true}, false)
	if !slices.Equal(got.Mechanical, []int{4, 5, 0}) {
		t.Fatalf("mechanical = %#v", got.Mechanical)
	}
	for _, index := range []int{1, 2, 3, 6, 7, 8, 9, 10} {
		if !slices.Contains(got.ReviewWorthy, index) {
			t.Fatalf("heuristic file %d was dropped: %#v", index, got)
		}
		if index > 5 && len(got.Flags[index]) == 0 {
			t.Fatalf("heuristic file %d was not flagged: %#v", index, got.Flags)
		}
	}
	included := Triage(files, map[int]bool{4: true}, true)
	if len(included.Mechanical) != 0 || len(included.ReviewWorthy) != len(files) {
		t.Fatalf("include-mechanical override failed: %#v", included)
	}
}

func TestPlanSummaryBatchesUsesFileAndCharacterCaps(t *testing.T) {
	files := make([]document.File, 26)
	indexes := make([]int, len(files))
	for index := range files {
		files[index] = testStageFile(index, fmt.Sprintf("src/%02d.go", index))
		indexes[index] = index
	}
	batches := PlanSummaryBatches(files, indexes, DefaultStageBudget)
	if len(batches) != 2 || len(batches[0].Files) != 25 || len(batches[1].Files) != 1 {
		t.Fatalf("file-capped batches = %#v", batches)
	}
	oneFileChars := summaryInputChars(0, files[0])
	batches = PlanSummaryBatches(files[:2], indexes[:2], oneFileChars)
	if len(batches) != 2 {
		t.Fatalf("character-capped batches = %#v", batches)
	}
}

func TestPlanSummaryBatchesShrinksOversizedSingleFileToBudget(t *testing.T) {
	file := testStageFile(0, "src/large.go")
	file.Hunks[0].Lines[0].Text = strings.Repeat("x", maxFileDiffCap)
	headerChars := len(fileHeader(0, file))
	budget := summaryBatchPromptOverheadChars() + headerChars + 50
	batches := PlanSummaryBatches([]document.File{file}, []int{0}, budget)
	if len(batches) != 1 || batches[0].InputChars > budget ||
		len(batches[0].DiffLimits) != 1 || batches[0].DiffLimits[0] != 50 {
		t.Fatalf("bounded batch = %#v, budget %d", batches, budget)
	}
	prompt := BuildSummaryBatchPrompt([]document.File{file}, batches[0], testDelimiters())
	if len(prompt) != batches[0].InputChars {
		t.Fatalf("prompt chars = %d, planned = %d", len(prompt), batches[0].InputChars)
	}
}

func TestSummaryBatchNeutralizationCannotExceedPlannedSize(t *testing.T) {
	file := testStageFile(0, "src/hostile.go")
	file.Hunks[0].Lines[0].Text = strings.Repeat("BEGIN_UNTRUSTED_CHANGE_DATA", 100)
	budget := summaryBatchPromptOverheadChars() + summaryInputChars(0, file)
	batch := PlanSummaryBatches([]document.File{file}, []int{0}, budget)[0]
	prompt := BuildSummaryBatchPrompt([]document.File{file}, batch, testDelimiters())
	if len(prompt) > batch.InputChars || len(prompt) > budget {
		t.Fatalf("neutralized prompt = %d, planned = %d, budget = %d",
			len(prompt), batch.InputChars, budget)
	}
}

func TestPlannerCallCountUsesActualFileCappedBatches(t *testing.T) {
	files := make([]document.File, 200)
	for index := range files {
		files[index] = testStageFile(index, fmt.Sprintf("src/file-%03d.go", index))
	}
	plan := PlanStaged(files, nil, false, 400_000)
	if len(plan.SummaryBatches) != 8 {
		t.Fatalf("summary batches = %d, want 8", len(plan.SummaryBatches))
	}
	// 8 summary batches + 2 flat cohort chunks + 1 synthesis.
	if plan.Calls != 11 {
		t.Fatalf("planned calls = %d, want 11", plan.Calls)
	}
}

func TestPlanCohortsSplitsNestedAndFlatDirectories(t *testing.T) {
	var files []document.File
	for index := 0; index < 151; index++ {
		files = append(files, document.File{Path: fmt.Sprintf("flat/file-%03d.go", index)})
	}
	for index := 0; index < 100; index++ {
		files = append(files, document.File{Path: fmt.Sprintf("nested/a/file-%03d.go", index)})
		files = append(files, document.File{Path: fmt.Sprintf("nested/b/file-%03d.go", index)})
	}
	cohorts := PlanCohorts(files)
	if len(cohorts) != 4 {
		t.Fatalf("cohort count = %d, want 4: %#v", len(cohorts), cohorts)
	}
	seen := make([]int, len(files))
	for _, cohort := range cohorts {
		if len(cohort.Files) > CohortMaxFiles {
			t.Fatalf("oversized cohort: %#v", cohort)
		}
		for _, index := range cohort.Files {
			seen[index]++
		}
	}
	for index, count := range seen {
		if count != 1 {
			t.Fatalf("file %d assigned %d times", index, count)
		}
	}
}

func TestCohortDigestPrioritizesFlaggedFilesWithoutGatingAnalysis(t *testing.T) {
	files := []document.File{
		{Path: "src/large.go", Additions: 100},
		{Path: "src/package-lock.json", Additions: 1},
		{Path: "src/medium.go", Additions: 50},
	}
	summaries := []FileSummary{
		{Summary: "large"}, {Summary: "flagged"}, {Summary: "medium"},
	}
	triage := Triage(files, nil, false)
	cohort := PlannedCohort{
		Title: "src backend changes", Layer: "backend", Directory: "src", Files: []int{0, 1, 2},
	}
	digest := BuildCohortDigest(files, cohort, triage, summaries, DefaultStageBudget)
	if strings.Index(digest, "package-lock.json") > strings.Index(digest, "large.go") {
		t.Fatalf("flagged file was not sampled first:\n%s", digest)
	}
	if len(digest) > DigestMaxChars {
		t.Fatalf("digest length = %d", len(digest))
	}
	plan := PlanStaged(files, nil, false, DefaultStageBudget)
	if len(plan.Triage.ReviewWorthy) != 3 {
		t.Fatalf("digest ranking gated analysis: %#v", plan.Triage)
	}
}

func TestAssembleStagedAnalysisPreservesMechanicalAndStubProvenance(t *testing.T) {
	files := []document.File{{Path: "a.go"}, {Path: "generated.go"}}
	plan := PlanStaged(files, map[int]bool{1: true}, false, DefaultStageBudget)
	summaries := []FileSummary{
		{Summary: "failed", LayerHint: "backend", KeySymbols: []string{}, Stubbed: true},
		mechanicalSummary(files[1], "generated"),
	}
	narrations := make([]CohortNarration, len(plan.Cohorts))
	for index := range narrations {
		narrations[index] = CohortNarration{
			Title: "Changes", Intent: "Review.", Narrative: "Review changes.", ReviewNotes: []string{},
		}
	}
	analysis := AssembleStagedAnalysis(files, plan, summaries, narrations, Synthesis{
		Title: "Change", Overview: "Overview",
	})
	if !slices.Equal(analysis.StubbedFiles, []int{0}) ||
		!slices.Equal(analysis.MechanicalFiles, []int{1}) {
		t.Fatalf("provenance = stubbed %#v mechanical %#v",
			analysis.StubbedFiles, analysis.MechanicalFiles)
	}
	if errors := ValidateComplete(analysis, len(files)); len(errors) > 0 {
		t.Fatalf("assembled analysis is invalid: %#v", errors)
	}
}
