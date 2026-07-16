package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/janiorvalle/better-git-review/internal/document"
)

type Mock struct{}

func (p *Mock) Name() string { return "mock" }

func (p *Mock) Detect() (bool, string) {
	return false, "mock is available only by explicit selection"
}

func (p *Mock) Complete(_ context.Context, prompt string) (string, error) {
	type promptFile struct {
		index     int
		path      string
		status    string
		additions string
		deletions string
	}
	var files []promptFile
	pattern := regexp.MustCompile(`(?m)^===== FILE (\d+): (.+) \(([^,]+), \+(\d+)/-(\d+)\) =====$`)
	for _, match := range pattern.FindAllStringSubmatch(prompt, -1) {
		index, _ := strconv.Atoi(match[1])
		path := match[2]
		if unquoted, err := strconv.Unquote(path); err == nil {
			path = unquoted
		}
		files = append(files, promptFile{
			index: index, path: path, status: match[3], additions: match[4], deletions: match[5],
		})
	}
	if len(files) == 0 {
		return "", fmt.Errorf("mock provider could not find files in analysis prompt")
	}
	groups := map[string][]promptFile{}
	for _, file := range files {
		layer := mockLayer(file.path)
		groups[layer] = append(groups[layer], file)
	}
	var cohorts []document.Cohort
	for _, layer := range document.Layers {
		group := groups[layer]
		if len(group) == 0 {
			continue
		}
		cohort := document.Cohort{
			Title:       strings.ToUpper(layer[:1]) + layer[1:] + " changes",
			Layer:       layer,
			Intent:      "[mock] Files grouped heuristically as " + layer + ".",
			Narrative:   "[mock mode] This grouping was produced by path heuristics, not an LLM.",
			ReviewNotes: []string{},
			DependsOn:   []int{},
		}
		for _, file := range group {
			cohort.Files = append(cohort.Files, file.index)
			cohort.FileSummaries = append(cohort.FileSummaries,
				fmt.Sprintf("[mock] %s, +%s/-%s", file.status, file.additions, file.deletions))
		}
		cohorts = append(cohorts, cohort)
	}
	sort.SliceStable(cohorts, func(i, j int) bool {
		return layerPosition(cohorts[i].Layer) < layerPosition(cohorts[j].Layer)
	})
	diagram := "graph LR\n  A[Mock mode] --> B[No LLM analysis]"
	analysis := document.Analysis{
		Title:    "[MOCK] Guided review",
		Overview: "Mock analysis: files were grouped by path heuristics only.",
		Mermaid:  &diagram,
		Cohorts:  cohorts,
	}
	encoded, err := json.Marshal(analysis)
	return string(encoded), err
}

func mockLayer(path string) string {
	lower := strings.ToLower(path)
	switch {
	case regexp.MustCompile(`migration|schema|\.sql$|models?/`).MatchString(lower):
		return "schema"
	case regexp.MustCompile(`test|spec|__tests__|\.test\.|\.spec\.`).MatchString(lower):
		return "tests"
	case regexp.MustCompile(`routes?|api|controller|endpoint|graphql|resolver`).MatchString(lower):
		return "api"
	case regexp.MustCompile(`component|page|view|\.css|\.scss|\.html$|frontend|ui/|\.tsx$|\.jsx$|\.vue$`).MatchString(lower):
		return "ui"
	case regexp.MustCompile(`\.(json|ya?ml|toml|ini|env|cfg)$|dockerfile|makefile|\.github/`).MatchString(lower):
		return "config"
	case regexp.MustCompile(`\.(md|rst|txt)$|docs?/`).MatchString(lower):
		return "docs"
	default:
		return "backend"
	}
}

func layerPosition(layer string) int {
	for i, candidate := range document.Layers {
		if layer == candidate {
			return i
		}
	}
	return len(document.Layers)
}
