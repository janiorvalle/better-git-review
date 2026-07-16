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

func TestStagedSummaryRetryAndStubDegradation(t *testing.T) {
	selected := &stagedTestProvider{
		fileCount:    2,
		invalidFirst: map[int]bool{0: true},
		failAlways:   map[int]bool{1: true},
	}
	analysis, err := Run(context.Background(), Options{
		Provider: selected,
		Source:   document.Source{Title: "staged"},
		Files: []document.File{
			testStageFile(0, "src/first.go"),
			testStageFile(1, "docs/broken.md"),
		},
		StateDir: t.TempDir(),
		Staged:   true,
		Random:   strings.NewReader("01234567"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if selected.attempts[0] != 2 || selected.attempts[1] != 2 || selected.clusterCalls != 1 {
		t.Fatalf("unexpected calls: attempts=%v cluster=%d", selected.attempts, selected.clusterCalls)
	}
	if len(analysis.StubbedFiles) != 1 || analysis.StubbedFiles[0] != 1 {
		t.Fatalf("stubbed files = %#v", analysis.StubbedFiles)
	}
	if !strings.Contains(selected.clusterPrompt, "path-derived stub") ||
		!strings.Contains(selected.clusterPrompt, "docs/broken.md") {
		t.Fatalf("stub was not passed to clustering:\n%s", selected.clusterPrompt)
	}
}

func TestStagedSummaryConcurrencyIsBounded(t *testing.T) {
	const fileCount = 12
	selected := &stagedTestProvider{fileCount: fileCount, delay: 25 * time.Millisecond}
	files := make([]document.File, fileCount)
	for index := range files {
		files[index] = testStageFile(index, fmt.Sprintf("src/file-%02d.go", index))
	}
	if _, err := Run(context.Background(), Options{
		Provider: selected,
		Source:   document.Source{Title: "concurrency"},
		Files:    files,
		StateDir: t.TempDir(),
		Staged:   true,
		Random:   strings.NewReader("01234567"),
	}); err != nil {
		t.Fatal(err)
	}
	if selected.highWater != StageConcurrency {
		t.Fatalf("concurrent high-water = %d, want %d", selected.highWater, StageConcurrency)
	}
}

type stagedTestProvider struct {
	mu            sync.Mutex
	fileCount     int
	invalidFirst  map[int]bool
	failAlways    map[int]bool
	attempts      map[int]int
	active        int
	highWater     int
	delay         time.Duration
	clusterCalls  int
	clusterPrompt string
}

func (p *stagedTestProvider) Name() string { return "staged-test" }
func (p *stagedTestProvider) Detect() (bool, string) {
	return true, "test"
}

func (p *stagedTestProvider) Complete(_ context.Context, prompt string) (string, error) {
	if strings.Contains(prompt, "STAGE: CLUSTER_SUMMARIES") {
		p.mu.Lock()
		p.clusterCalls++
		p.clusterPrompt = prompt
		p.mu.Unlock()
		files := make([]int, p.fileCount)
		summaries := make([]string, p.fileCount)
		for index := range files {
			files[index] = index
			summaries[index] = "summary"
		}
		encoded, _ := json.Marshal(document.Analysis{
			Title: "staged", Overview: "clustered summaries",
			Cohorts: []document.Cohort{{
				Title: "Changes", Layer: "backend", Intent: "intent", Narrative: "narrative",
				Files: files, FileSummaries: summaries, ReviewNotes: []string{}, DependsOn: []int{},
			}},
		})
		return string(encoded), nil
	}
	match := regexp.MustCompile(`(?m)^===== FILE (\d+):`).FindStringSubmatch(prompt)
	if len(match) != 2 {
		return "", fmt.Errorf("summary prompt did not contain a file index")
	}
	index, _ := strconv.Atoi(match[1])
	p.mu.Lock()
	if p.attempts == nil {
		p.attempts = map[int]int{}
	}
	p.attempts[index]++
	attempt := p.attempts[index]
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

	if p.failAlways[index] || (p.invalidFirst[index] && attempt == 1) {
		return `{"summary":`, nil
	}
	return fmt.Sprintf(`{"summary":"file %d","layerHint":"backend","keySymbols":[]}`, index), nil
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
