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
	"github.com/janiorvalle/better-git-review/internal/provider"
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
}

type FileSummary struct {
	Summary    string   `json:"summary"`
	LayerHint  string   `json:"layerHint"`
	KeySymbols []string `json:"keySymbols"`
	Stubbed    bool     `json:"-"`
}

var SummarySchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["summary", "layerHint", "keySymbols"],
  "properties": {
    "summary": {"type": "string"},
    "layerHint": {"type": "string", "enum": ["schema", "backend", "api", "ui", "tests", "config", "docs", "other"]},
    "keySymbols": {"type": "array", "items": {"type": "string"}}
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
	logf("staged analysis: summarizing %d file(s) with up to %d concurrent calls", len(opts.Files), StageConcurrency)
	summaries := make([]FileSummary, len(opts.Files))
	summaryErrors := make([]error, len(opts.Files))
	jobs := make(chan int)
	var workers sync.WaitGroup
	workerCount := min(StageConcurrency, len(opts.Files))
	for range workerCount {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for index := range jobs {
				prompt := BuildFileSummaryPrompt(opts.Files[index], index, delimiters)
				summary, _, _, err := runGauntlet(
					ctx,
					opts.Provider,
					prompt,
					SummarySchema,
					fmt.Sprintf("file %d summary", index),
					validateFileSummary,
					logf,
				)
				if err != nil {
					summaryErrors[index] = err
					summaries[index] = stubSummary(opts.Files[index])
					continue
				}
				summaries[index] = summary
			}
		}()
	}
	for index := range opts.Files {
		jobs <- index
	}
	close(jobs)
	workers.Wait()
	if err := ctx.Err(); err != nil {
		return document.Analysis{}, err
	}

	stubbed := make([]int, 0)
	for index, summaryErr := range summaryErrors {
		if summaryErr == nil {
			continue
		}
		stubbed = append(stubbed, index)
		logf("file %d summary failed; using path-derived stub: %v", index, summaryErr)
	}
	logf("staged analysis: clustering %d file summaries", len(summaries))
	prompt := BuildClusterPrompt(opts.Source, opts.Files, summaries, delimiters)
	analysis, raw, validationErrors, err := runGauntlet(
		ctx,
		opts.Provider,
		prompt,
		Schema,
		"summary clustering",
		func(value document.Analysis) []string {
			return validateBeforeSeatbelts(value, len(opts.Files))
		},
		logf,
	)
	if err != nil {
		return document.Analysis{}, analysisFailure(opts.StateDir, raw, validationErrors, err)
	}
	analysis = ApplySeatbelts(analysis, len(opts.Files))
	analysis.StubbedFiles = stubbed
	if validationErrors := ValidateComplete(analysis, len(opts.Files)); len(validationErrors) > 0 {
		return document.Analysis{}, analysisFailure(opts.StateDir, raw, validationErrors, nil)
	}
	return analysis, nil
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
			logf("provider output for %s failed validation; retrying once ...", unit)
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

func validateFileSummary(summary FileSummary) []string {
	var errors []string
	if strings.TrimSpace(summary.Summary) == "" {
		errors = append(errors, "summary must not be empty")
	}
	if !document.IsLayer(summary.LayerHint) {
		errors = append(errors, "layerHint is not in the allowed enum")
	}
	if summary.KeySymbols == nil {
		errors = append(errors, "keySymbols must be present")
	}
	return errors
}

func stubSummary(file document.File) FileSummary {
	return FileSummary{
		Summary: fmt.Sprintf("Change in %s (%s, +%d/-%d); no model summary was available.",
			file.Path, file.Status, file.Additions, file.Deletions),
		LayerHint:  pathLayerHint(file.Path),
		KeySymbols: []string{},
		Stubbed:    true,
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
		stateDir, err = DefaultStateDir()
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

func DefaultStateDir() (string, error) {
	if value := os.Getenv("XDG_STATE_HOME"); value != "" {
		return filepath.Join(value, "better-git-review"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", "better-git-review"), nil
}
