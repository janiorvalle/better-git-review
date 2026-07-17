package analyze

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/janiorvalle/better-git-review/internal/document"
)

func TestStagedBatchRetryStubsWholeBatch(t *testing.T) {
	files := []document.File{
		testStageFile(0, "src/first.go"),
		testStageFile(1, "docs/broken.md"),
	}
	plan := PlanStaged(files, nil, false, DefaultStageBudget)
	selected := &stagedTestProvider{failSummary: true}
	analysis, err := Run(context.Background(), Options{
		Provider: selected,
		Source:   document.Source{Title: "staged"},
		Files:    files,
		StateDir: t.TempDir(),
		Staged:   true,
		Budget:   DefaultStageBudget,
		Plan:     &plan,
		Random:   strings.NewReader("01234567"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if selected.summaryCalls != 2 || selected.narrationCalls != len(plan.Cohorts) || selected.synthesisCalls != 1 {
		t.Fatalf("unexpected calls: summary=%d narration=%d synthesis=%d",
			selected.summaryCalls, selected.narrationCalls, selected.synthesisCalls)
	}
	if fmt.Sprint(analysis.StubbedFiles) != "[1 0]" && fmt.Sprint(analysis.StubbedFiles) != "[0 1]" {
		t.Fatalf("stubbed files = %#v", analysis.StubbedFiles)
	}
	if len(analysis.MechanicalFiles) != 0 {
		t.Fatalf("mechanical files = %#v", analysis.MechanicalFiles)
	}
}

func TestStagedSummaryBatchConcurrencyIsBounded(t *testing.T) {
	const fileCount = 125
	files := make([]document.File, fileCount)
	for index := range files {
		files[index] = testStageFile(index, fmt.Sprintf("src/file-%03d.go", index))
	}
	plan := PlanStaged(files, nil, false, DefaultStageBudget)
	if len(plan.SummaryBatches) != 5 {
		t.Fatalf("summary batches = %d, want 5", len(plan.SummaryBatches))
	}
	selected := &stagedTestProvider{delay: 25 * time.Millisecond}
	if _, err := Run(context.Background(), Options{
		Provider: selected,
		Source:   document.Source{Title: "concurrency"},
		Files:    files,
		StateDir: t.TempDir(),
		Staged:   true,
		Budget:   DefaultStageBudget,
		Plan:     &plan,
		Random:   strings.NewReader("01234567"),
	}); err != nil {
		t.Fatal(err)
	}
	if selected.highWater != StageConcurrency {
		t.Fatalf("concurrent high-water = %d, want %d", selected.highWater, StageConcurrency)
	}
	if got := selected.summaryCalls + selected.narrationCalls + selected.synthesisCalls; got != plan.Calls {
		t.Fatalf("actual calls = %d, planned = %d", got, plan.Calls)
	}
}

func TestStagedRetryPromptNeverExceedsPlannedBudget(t *testing.T) {
	files := []document.File{testStageFile(0, "src/retry.go")}
	budget := minimumStagedBudget(files) + 500
	plan := PlanStaged(files, nil, false, budget)
	selected := &stagedTestProvider{invalidSummaryOnce: true}
	if _, err := Run(context.Background(), Options{
		Provider: selected,
		Source:   document.Source{Title: "bounded retry"},
		Files:    files,
		StateDir: t.TempDir(),
		Staged:   true,
		Budget:   budget,
		Plan:     &plan,
		Random:   strings.NewReader("01234567"),
	}); err != nil {
		t.Fatal(err)
	}
	if selected.summaryCalls != 2 {
		t.Fatalf("summary calls = %d, want retry", selected.summaryCalls)
	}
	for index, promptLength := range selected.promptLengths {
		if promptLength > budget {
			t.Fatalf("prompt %d length = %d, budget = %d", index, promptLength, budget)
		}
	}
}

type stagedTestProvider struct {
	mu                 sync.Mutex
	failSummary        bool
	invalidSummaryOnce bool
	delay              time.Duration
	active             int
	highWater          int
	summaryCalls       int
	narrationCalls     int
	synthesisCalls     int
	promptLengths      []int
}

func (p *stagedTestProvider) Name() string { return "staged-test" }
func (p *stagedTestProvider) Detect() (bool, string) {
	return true, "test"
}

func (p *stagedTestProvider) Complete(_ context.Context, prompt string) (string, error) {
	p.mu.Lock()
	p.promptLengths = append(p.promptLengths, len(prompt))
	p.mu.Unlock()
	switch {
	case strings.Contains(prompt, "STAGE: SUMMARY_BATCH"):
		p.mu.Lock()
		p.summaryCalls++
		summaryCall := p.summaryCalls
		p.active++
		if p.active > p.highWater {
			p.highWater = p.active
		}
		p.mu.Unlock()
		if p.delay > 0 {
			time.Sleep(p.delay)
		}
		p.mu.Lock()
		p.active--
		p.mu.Unlock()
		if p.failSummary || p.invalidSummaryOnce && summaryCall == 1 {
			return `{"summary":`, nil
		}
		matches := regexp.MustCompile(`(?m)^===== FILE (\d+):`).FindAllStringSubmatch(prompt, -1)
		result := make([]BatchSummary, 0, len(matches))
		for _, match := range matches {
			index, _ := strconv.Atoi(match[1])
			result = append(result, BatchSummary{
				Index: index, Summary: fmt.Sprintf("file %d", index),
				LayerHint: "backend", KeySymbols: []string{},
			})
		}
		encoded, _ := json.Marshal(result)
		return string(encoded), nil
	case strings.Contains(prompt, "STAGE: COHORT_NARRATE"):
		p.mu.Lock()
		p.narrationCalls++
		p.mu.Unlock()
		return `{"title":"Changes","intent":"Review changes.","narrative":"Review the grouped files.","reviewNotes":[]}`, nil
	case strings.Contains(prompt, "STAGE: SYNTHESIS"):
		p.mu.Lock()
		p.synthesisCalls++
		p.mu.Unlock()
		return `{"title":"Staged","overview":"Staged analysis."}`, nil
	default:
		return "", fmt.Errorf("unexpected prompt stage")
	}
}

func testStageFile(index int, path string) document.File {
	return document.File{
		Path: path, Status: "modified", Additions: 1,
		Hunks: []document.Hunk{{
			Header: fmt.Sprintf("file-%d", index),
			Lines:  []document.HunkLine{{Type: "a", New: 1, Text: "changed"}},
		}},
	}
}
