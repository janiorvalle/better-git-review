package mock

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/janiorvalle/better-git-review/internal/pathlayer"
	"github.com/janiorvalle/better-git-review/internal/provider"
)

type Adapter struct{}

func (Adapter) Name() string {
	return "mock"
}

func (Adapter) New(opts provider.AdapterOptions) (provider.Provider, string, string, []string, error) {
	model := provider.ChooseModel(opts.ModelOverride, opts.ConfiguredModel, "deterministic")
	reasoning := provider.ChooseReasoning(opts.ReasoningOverride, opts.ConfiguredReasoning, "")
	return &Provider{Getenv: opts.Getenv, Reasoning: reasoning}, model, reasoning, nil, nil
}

type Provider struct {
	Getenv    func(string) string
	Reasoning string
	mu        sync.Mutex
}

func (p *Provider) Name() string { return "mock" }

func (p *Provider) Detect() (bool, string) {
	return false, "mock is available only by explicit selection"
}

func (p *Provider) Complete(_ context.Context, prompt string) (string, error) {
	if err := p.recordReasoning(); err != nil {
		return "", err
	}
	if err := p.recordPrompt(prompt); err != nil {
		return "", err
	}
	if strings.Contains(prompt, "STAGE: SYNTHESIS") {
		return `{"title":"[MOCK] Guided review","overview":"Mock staged analysis: deterministic cohorts with bounded narration."}`, nil
	}
	if strings.Contains(prompt, "STAGE: COHORT_NARRATE") {
		return `{"title":"[mock] Cohort","intent":"[mock] Review the deterministically grouped files.","narrative":"[mock mode] This bounded narration was produced without an LLM.","reviewNotes":[]}`, nil
	}
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
	if strings.Contains(prompt, "STAGE: SUMMARY_BATCH") {
		result := make([]map[string]any, 0, len(files))
		for _, file := range files {
			if failure := p.getenv("BGR_MOCK_FAIL_SUMMARY"); failure != "" && strings.Contains(file.path, failure) {
				return `[{"index":`, nil
			}
			result = append(result, map[string]any{
				"index":      file.index,
				"summary":    fmt.Sprintf("[mock] %s, +%s/-%s", file.status, file.additions, file.deletions),
				"layerHint":  pathlayer.Classify(file.path),
				"keySymbols": []string{},
			})
		}
		return string(mustJSON(result)), nil
	}
	groups := map[string][]promptFile{}
	for _, file := range files {
		layer := pathlayer.Classify(file.path)
		groups[layer] = append(groups[layer], file)
	}
	var cohorts []cohort
	for _, layer := range layers {
		group := groups[layer]
		if len(group) == 0 {
			continue
		}
		item := cohort{
			Title:       strings.ToUpper(layer[:1]) + layer[1:] + " changes",
			Layer:       layer,
			Intent:      "[mock] Files grouped heuristically as " + layer + ".",
			Narrative:   "[mock mode] This grouping was produced by path heuristics, not an LLM.",
			ReviewNotes: []string{},
			DependsOn:   []int{},
		}
		for _, file := range group {
			item.Files = append(item.Files, file.index)
			item.FileSummaries = append(item.FileSummaries,
				fmt.Sprintf("[mock] %s, +%s/-%s", file.status, file.additions, file.deletions))
		}
		cohorts = append(cohorts, item)
	}
	sort.SliceStable(cohorts, func(i, j int) bool {
		return layerPosition(cohorts[i].Layer) < layerPosition(cohorts[j].Layer)
	})
	analysis := struct {
		Title    string   `json:"title"`
		Overview string   `json:"overview"`
		Cohorts  []cohort `json:"cohorts"`
	}{
		Title:    "[MOCK] Guided review",
		Overview: "Mock analysis: files were grouped by path heuristics only.",
		Cohorts:  cohorts,
	}
	encoded, err := json.Marshal(analysis)
	return string(encoded), err
}

func (p *Provider) recordReasoning() error {
	path := p.getenv("BGR_MOCK_REASONING_LOG")
	if path == "" {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return os.WriteFile(path, []byte(p.Reasoning+"\n"), 0o600)
}

type cohort struct {
	Title         string   `json:"title"`
	Layer         string   `json:"layer"`
	Intent        string   `json:"intent"`
	Narrative     string   `json:"narrative"`
	Files         []int    `json:"files"`
	FileSummaries []string `json:"fileSummaries"`
	ReviewNotes   []string `json:"reviewNotes"`
	DependsOn     []int    `json:"dependsOn"`
}

var layers = []string{"schema", "backend", "api", "ui", "tests", "config", "docs", "other"}

func (p *Provider) getenv(name string) string {
	if p.Getenv != nil {
		return p.Getenv(name)
	}
	return os.Getenv(name)
}

func (p *Provider) recordPrompt(prompt string) error {
	path := p.getenv("BGR_MOCK_PROMPT_LOG")
	if path == "" {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = fmt.Fprintf(file, "----- MOCK PROMPT -----\n%s\n", prompt)
	return err
}

func mustJSON(value any) []byte {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return encoded
}

func layerPosition(layer string) int {
	for i, candidate := range layers {
		if layer == candidate {
			return i
		}
	}
	return len(layers)
}
