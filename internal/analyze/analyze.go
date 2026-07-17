package analyze

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/janiorvalle/better-git-review/internal/document"
	"github.com/janiorvalle/better-git-review/internal/pathlayer"
	"github.com/janiorvalle/better-git-review/internal/provider"
	"github.com/janiorvalle/better-git-review/internal/xdg"
)

const StageConcurrency = 4

type Options struct {
	Provider provider.Provider
	Source   document.Source
	Files    []document.File
	StateDir string
	Logf     func(string, ...any)
	Staged   bool
	Budget   int
	Random   io.Reader
	Progress func(completed, total int)
	Plan     *StagedPlan
}

type FileSummary struct {
	Summary    string   `json:"summary"`
	LayerHint  string   `json:"layerHint"`
	KeySymbols []string `json:"keySymbols"`
	Stubbed    bool     `json:"-"`
}

type BatchSummary struct {
	Index      int      `json:"index"`
	Summary    string   `json:"summary"`
	LayerHint  string   `json:"layerHint"`
	KeySymbols []string `json:"keySymbols"`
}

type CohortNarration struct {
	Title       string   `json:"title"`
	Intent      string   `json:"intent"`
	Narrative   string   `json:"narrative"`
	ReviewNotes []string `json:"reviewNotes"`
}

type Synthesis struct {
	Title    string `json:"title"`
	Overview string `json:"overview"`
}

var BatchSummarySchema = json.RawMessage(`{
  "type": "array",
  "items": {
    "type": "object",
    "additionalProperties": false,
    "required": ["index", "summary", "layerHint", "keySymbols"],
    "properties": {
      "index": {"type": "integer", "minimum": 0},
      "summary": {"type": "string"},
      "layerHint": {"type": "string", "enum": ["schema", "backend", "api", "ui", "tests", "config", "docs", "other"]},
      "keySymbols": {"type": "array", "items": {"type": "string"}}
    }
  }
}`)

var CohortNarrationSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["title", "intent", "narrative", "reviewNotes"],
  "properties": {
    "title": {"type": "string"},
    "intent": {"type": "string"},
    "narrative": {"type": "string"},
    "reviewNotes": {"type": "array", "items": {"type": "string"}}
  }
}`)

var SynthesisSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["title", "overview"],
  "properties": {
    "title": {"type": "string"},
    "overview": {"type": "string"}
  }
}`)

func Run(ctx context.Context, opts Options) (document.Analysis, error) {
	if opts.Logf == nil {
		opts.Logf = func(string, ...any) {}
	}
	var logMu sync.Mutex
	logf := func(format string, args ...any) {
		logMu.Lock()
		defer logMu.Unlock()
		opts.Logf(format, args...)
	}
	delimiters, err := NewDelimiters(opts.Random)
	if err != nil {
		return document.Analysis{}, err
	}
	if opts.Staged {
		return runStaged(ctx, opts, delimiters, logf)
	}
	prompt := BuildPrompt(opts.Source, opts.Files, opts.Budget, delimiters)
	analysis, raw, validationErrors, err := runGauntlet(
		ctx,
		opts.Provider,
		prompt,
		Schema,
		"analysis",
		func(value document.Analysis) []string {
			return validateBeforeSeatbelts(value, len(opts.Files))
		},
		logf,
	)
	if err != nil {
		return document.Analysis{}, analysisFailure(opts.StateDir, raw, validationErrors, err)
	}
	analysis = ApplySeatbelts(analysis, len(opts.Files))
	if validationErrors := ValidateComplete(analysis, len(opts.Files)); len(validationErrors) > 0 {
		return document.Analysis{}, analysisFailure(opts.StateDir, raw, validationErrors, nil)
	}
	return analysis, nil
}

func runStaged(
	ctx context.Context,
	opts Options,
	delimiters Delimiters,
	logf func(string, ...any),
) (document.Analysis, error) {
	plan := opts.Plan
	if plan == nil {
		value := PlanStaged(opts.Files, nil, false, opts.Budget)
		plan = &value
	}
	logf("summarizing %d review-worthy files in %d batches, up to %d batches at a time",
		len(plan.Triage.ReviewWorthy), len(plan.SummaryBatches), StageConcurrency)
	summaries := make([]FileSummary, len(opts.Files))
	for _, index := range plan.Triage.Mechanical {
		summaries[index] = mechanicalSummary(opts.Files[index], plan.Triage.MechanicalWhy[index])
	}
	summaryErrors := make([]error, len(plan.SummaryBatches))
	jobs := make(chan int)
	var workers sync.WaitGroup
	var progressMu sync.Mutex
	var completed int
	workerCount := min(StageConcurrency, len(plan.SummaryBatches))
	for range workerCount {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for batchIndex := range jobs {
				batch := plan.SummaryBatches[batchIndex]
				prompt := BuildSummaryBatchPrompt(opts.Files, batch, delimiters)
				result, _, _, err := runStageAttempts(
					ctx,
					opts.Provider,
					prompt,
					BatchSummarySchema,
					opts.Budget,
					fmt.Sprintf("summary batch %d", batchIndex+1),
					func(value []BatchSummary) []string {
						return validateBatchSummaries(value, batch.Files)
					},
					logf,
				)
				if err != nil {
					summaryErrors[batchIndex] = err
					for _, fileIndex := range batch.Files {
						summaries[fileIndex] = stubSummary(opts.Files[fileIndex])
					}
				} else {
					for _, summary := range result {
						summaries[summary.Index] = FileSummary{
							Summary: summary.Summary, LayerHint: summary.LayerHint,
							KeySymbols: summary.KeySymbols,
						}
					}
				}
				progressMu.Lock()
				completed++
				if opts.Progress != nil {
					opts.Progress(completed, len(plan.SummaryBatches))
				}
				progressMu.Unlock()
			}
		}()
	}
	for batchIndex := range plan.SummaryBatches {
		jobs <- batchIndex
	}
	close(jobs)
	workers.Wait()
	if err := ctx.Err(); err != nil {
		return document.Analysis{}, err
	}

	for batchIndex, summaryErr := range summaryErrors {
		if summaryErr == nil {
			continue
		}
		logf("summary batch %d failed; stubbing all %d files: %v",
			batchIndex+1, len(plan.SummaryBatches[batchIndex].Files), summaryErr)
	}

	logf("narrating %d deterministic cohorts", len(plan.Cohorts))
	narrations := make([]CohortNarration, len(plan.Cohorts))
	for cohortIndex, cohort := range plan.Cohorts {
		digest := BuildCohortDigest(
			opts.Files,
			cohort,
			plan.Triage,
			summaries,
			cohortDigestBudget(opts.Budget, cohort, delimiters),
		)
		prompt := BuildCohortNarrationPrompt(cohort, digest, delimiters)
		narration, raw, validationErrors, err := runStageAttempts(
			ctx,
			opts.Provider,
			prompt,
			CohortNarrationSchema,
			opts.Budget,
			fmt.Sprintf("cohort %d narration", cohortIndex+1),
			validateCohortNarration,
			logf,
		)
		if err != nil {
			return document.Analysis{}, analysisFailure(opts.StateDir, raw, validationErrors, err)
		}
		narrations[cohortIndex] = narration
	}

	synthesisPrompt := BuildSynthesisPrompt(opts.Source, plan.Cohorts, narrations, opts.Budget, delimiters)
	synthesis, raw, validationErrors, err := runStageAttempts(
		ctx,
		opts.Provider,
		synthesisPrompt,
		SynthesisSchema,
		opts.Budget,
		"cohort synthesis",
		validateSynthesis,
		logf,
	)
	if err != nil {
		return document.Analysis{}, analysisFailure(opts.StateDir, raw, validationErrors, err)
	}

	analysis := AssembleStagedAnalysis(opts.Files, *plan, summaries, narrations, synthesis)
	if validationErrors := ValidateComplete(analysis, len(opts.Files)); len(validationErrors) > 0 {
		return document.Analysis{}, fmt.Errorf(
			"internal staged assembly invariant failed: %s", FormatErrors(validationErrors))
	}
	return analysis, nil
}

func runStageAttempts[T any](
	ctx context.Context,
	selected provider.Provider,
	prompt string,
	schema json.RawMessage,
	budget int,
	unit string,
	validate func(T) []string,
	logf func(string, ...any),
) (T, string, []string, error) {
	var zero T
	var lastRaw string
	var lastErrors []string
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		attemptPrompt := prompt
		if attempt > 0 {
			logf("the model's %s call failed - retrying once ...", unit)
			if len(lastErrors) > 0 {
				correction := "\n\nYour previous response failed validation. Return corrected JSON only. Exact errors:\n- " +
					strings.Join(lastErrors, "\n- ")
				if remaining := budget - len(attemptPrompt); remaining > 0 {
					attemptPrompt += correction[:min(len(correction), remaining)]
				}
			}
		}
		var value T
		if structured, ok := selected.(provider.StructuredProvider); ok {
			raw, err := structured.CompleteStructured(ctx, attemptPrompt, schema)
			if err != nil {
				lastErr = err
				if ctx.Err() != nil {
					return zero, lastRaw, lastErrors, ctx.Err()
				}
				continue
			}
			lastRaw = string(raw)
			if err := json.Unmarshal(raw, &value); err != nil {
				lastErrors = []string{"structured response could not be decoded: " + err.Error()}
				continue
			}
		} else {
			raw, err := selected.Complete(ctx, attemptPrompt)
			if err != nil {
				lastErr = err
				if ctx.Err() != nil {
					return zero, lastRaw, lastErrors, ctx.Err()
				}
				continue
			}
			lastRaw = raw
			if err := ParseResponseInto(raw, &value); err != nil {
				lastErrors = []string{err.Error()}
				continue
			}
		}
		if validationErrors := validate(value); len(validationErrors) > 0 {
			lastErrors = validationErrors
			continue
		}
		return value, lastRaw, nil, nil
	}
	if lastErr != nil && len(lastErrors) == 0 {
		return zero, lastRaw, nil, fmt.Errorf("%s provider failed after 2 attempts: %w", selected.Name(), lastErr)
	}
	return zero, lastRaw, lastErrors, fmt.Errorf(
		"provider output failed after 2 attempts: %s", FormatErrors(lastErrors))
}

func runGauntlet[T any](
	ctx context.Context,
	selected provider.Provider,
	prompt string,
	schema json.RawMessage,
	unit string,
	validate func(T) []string,
	logf func(string, ...any),
) (T, string, []string, error) {
	var zero T
	var lastRaw string
	var lastErrors []string
	for attempt := 0; attempt < 2; attempt++ {
		attemptPrompt := prompt
		if attempt > 0 {
			attemptPrompt += "\n\nYour previous response failed validation. Return corrected JSON only. Exact errors:\n- " +
				strings.Join(lastErrors, "\n- ")
			logf("the model's %s answer didn't validate - asking for a corrected one ...", unit)
		}

		var value T
		if structured, ok := selected.(provider.StructuredProvider); ok {
			raw, err := structured.CompleteStructured(ctx, attemptPrompt, schema)
			if err != nil {
				return zero, lastRaw, lastErrors, fmt.Errorf("%s provider failed: %w", selected.Name(), err)
			}
			lastRaw = string(raw)
			if err := json.Unmarshal(raw, &value); err != nil {
				lastErrors = []string{"structured response could not be decoded: " + err.Error()}
				continue
			}
		} else {
			raw, err := selected.Complete(ctx, attemptPrompt)
			lastRaw = raw
			if err != nil {
				return zero, lastRaw, lastErrors, fmt.Errorf("%s provider failed: %w", selected.Name(), err)
			}
			if err := ParseResponseInto(raw, &value); err != nil {
				lastErrors = []string{err.Error()}
				continue
			}
		}
		if validationErrors := validate(value); len(validationErrors) > 0 {
			lastErrors = validationErrors
			continue
		}
		return value, lastRaw, nil, nil
	}
	return zero, lastRaw, lastErrors, fmt.Errorf("provider output failed after 2 attempts: %s", FormatErrors(lastErrors))
}

func validateBatchSummaries(summaries []BatchSummary, expected []int) []string {
	var errors []string
	if len(summaries) != len(expected) {
		errors = append(errors, fmt.Sprintf(
			"summary batch returned %d items, want %d", len(summaries), len(expected)))
	}
	expectedSet := make(map[int]bool, len(expected))
	for _, index := range expected {
		expectedSet[index] = true
	}
	seen := map[int]bool{}
	for position, summary := range summaries {
		prefix := fmt.Sprintf("summaries[%d]", position)
		if !expectedSet[summary.Index] {
			errors = append(errors, fmt.Sprintf("%s.index %d was not requested", prefix, summary.Index))
		}
		if seen[summary.Index] {
			errors = append(errors, fmt.Sprintf("%s.index %d is duplicated", prefix, summary.Index))
		}
		seen[summary.Index] = true
		if strings.TrimSpace(summary.Summary) == "" {
			errors = append(errors, prefix+".summary must not be empty")
		}
		if !document.IsLayer(summary.LayerHint) {
			errors = append(errors, prefix+".layerHint is not in the allowed enum")
		}
		if summary.KeySymbols == nil {
			errors = append(errors, prefix+".keySymbols must be present")
		}
	}
	for _, index := range expected {
		if !seen[index] {
			errors = append(errors, fmt.Sprintf("file index %d is missing from summary batch", index))
		}
	}
	return errors
}

func validateCohortNarration(narration CohortNarration) []string {
	var errors []string
	if strings.TrimSpace(narration.Title) == "" {
		errors = append(errors, "title must not be empty")
	}
	if strings.TrimSpace(narration.Intent) == "" {
		errors = append(errors, "intent must not be empty")
	}
	if strings.TrimSpace(narration.Narrative) == "" {
		errors = append(errors, "narrative must not be empty")
	}
	if narration.ReviewNotes == nil {
		errors = append(errors, "reviewNotes must be present")
	}
	return errors
}

func validateSynthesis(synthesis Synthesis) []string {
	var errors []string
	if strings.TrimSpace(synthesis.Title) == "" {
		errors = append(errors, "title must not be empty")
	}
	if strings.TrimSpace(synthesis.Overview) == "" {
		errors = append(errors, "overview must not be empty")
	}
	return errors
}

func stubSummary(file document.File) FileSummary {
	return FileSummary{
		Summary: fmt.Sprintf("Change in %s (%s, +%d/-%d); no model summary was available.",
			file.Path, file.Status, file.Additions, file.Deletions),
		LayerHint:  pathlayer.Classify(file.Path),
		KeySymbols: []string{},
		Stubbed:    true,
	}
}

func mechanicalSummary(file document.File, reason string) FileSummary {
	if reason == "" {
		reason = "mechanical change"
	}
	return FileSummary{
		Summary: fmt.Sprintf("%s: %s; model analysis was deliberately skipped.",
			file.Path, reason),
		LayerHint:  pathlayer.Classify(file.Path),
		KeySymbols: []string{},
	}
}

func analysisFailure(stateDir, raw string, validationErrors []string, cause error) error {
	if cause != nil && !strings.Contains(cause.Error(), "failed after 2 attempts") {
		return cause
	}
	debugPath, debugErr := writeDebugOutput(stateDir, raw)
	if debugErr != nil {
		return fmt.Errorf("provider output failed after 2 attempts (%s); also failed to write debug output: %v",
			FormatErrors(validationErrors), debugErr)
	}
	return fmt.Errorf("provider output failed after 2 attempts: %s; raw output saved to %s",
		FormatErrors(validationErrors), debugPath)
}

func writeDebugOutput(stateDir, raw string) (string, error) {
	if stateDir == "" {
		var err error
		stateDir, err = xdg.StateDir()
		if err != nil {
			return "", err
		}
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(stateDir, "debug-"+time.Now().UTC().Format("20060102T150405.000000000Z")+".txt")
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		return "", err
	}
	return path, nil
}
