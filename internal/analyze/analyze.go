package analyze

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/janiorvalle/better-git-review/internal/document"
	"github.com/janiorvalle/better-git-review/internal/provider"
)

type Options struct {
	Provider provider.Provider
	Source   document.Source
	Files    []document.File
	StateDir string
	Logf     func(string, ...any)
}

func Run(ctx context.Context, opts Options) (document.Analysis, error) {
	if opts.Logf == nil {
		opts.Logf = func(string, ...any) {}
	}
	prompt := BuildPrompt(opts.Source, opts.Files)
	var lastRaw string
	var lastErrors []string

	for attempt := 0; attempt < 2; attempt++ {
		attemptPrompt := prompt
		if attempt > 0 {
			attemptPrompt += "\n\nYour previous response failed validation. Return corrected JSON only. Exact errors:\n- " +
				strings.Join(lastErrors, "\n- ")
			opts.Logf("provider output failed validation; retrying once ...")
		}

		var analysis document.Analysis
		if structured, ok := opts.Provider.(provider.StructuredProvider); ok {
			raw, err := structured.CompleteStructured(ctx, attemptPrompt, Schema)
			if err != nil {
				return document.Analysis{}, fmt.Errorf("%s provider failed: %w", opts.Provider.Name(), err)
			}
			lastRaw = string(raw)
			if err := json.Unmarshal(raw, &analysis); err != nil {
				lastErrors = []string{"structured response could not be decoded: " + err.Error()}
				continue
			}
		} else {
			raw, err := opts.Provider.Complete(ctx, attemptPrompt)
			lastRaw = raw
			if err != nil {
				return document.Analysis{}, fmt.Errorf("%s provider failed: %w", opts.Provider.Name(), err)
			}
			analysis, err = ParseResponse(raw)
			if err != nil {
				lastErrors = []string{err.Error()}
				continue
			}
		}

		if validationErrors := validateBeforeSeatbelts(analysis, len(opts.Files)); len(validationErrors) > 0 {
			lastErrors = validationErrors
			continue
		}
		analysis = ApplySeatbelts(analysis, len(opts.Files))
		if validationErrors := ValidateComplete(analysis, len(opts.Files)); len(validationErrors) > 0 {
			lastErrors = validationErrors
			continue
		}
		return analysis, nil
	}

	debugPath, debugErr := writeDebugOutput(opts.StateDir, lastRaw)
	if debugErr != nil {
		return document.Analysis{}, fmt.Errorf("provider output failed after 2 attempts (%s); also failed to write debug output: %v",
			FormatErrors(lastErrors), debugErr)
	}
	return document.Analysis{}, fmt.Errorf("provider output failed after 2 attempts: %s; raw output saved to %s",
		FormatErrors(lastErrors), debugPath)
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
