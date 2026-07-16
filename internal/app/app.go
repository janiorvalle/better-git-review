package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/janiorvalle/better-git-review/internal/analyze"
	"github.com/janiorvalle/better-git-review/internal/cache"
	"github.com/janiorvalle/better-git-review/internal/config"
	"github.com/janiorvalle/better-git-review/internal/diff"
	"github.com/janiorvalle/better-git-review/internal/document"
	"github.com/janiorvalle/better-git-review/internal/guard"
	"github.com/janiorvalle/better-git-review/internal/provider"
	"github.com/janiorvalle/better-git-review/internal/source"
)

type Environment struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	Getwd  func() (string, error)
}

type options struct {
	PR              string
	DiffFile        string
	Base            string
	RepoDir         string
	Provider        string
	Model           string
	Out             string
	NoCache         bool
	Yes             bool
	TrustRepoConfig bool
}

func Run(ctx context.Context, args []string, env Environment) error {
	env = fillEnvironment(env)
	opts, err := parseArgs(args, env)
	if err != nil {
		return err
	}

	repoRoot := source.RepoRoot(ctx, opts.RepoDir)
	loadedConfig, err := config.Load(config.LoadOptions{
		RepoDir:         repoRoot,
		Flags:           config.Flags{Provider: opts.Provider},
		AcceptRepoTrust: opts.TrustRepoConfig,
		Yes:             opts.Yes,
		Input:           env.Stdin,
		Output:          env.Stderr,
		InputIsTTY:      isTTY(env.Stdin),
	})
	if err != nil {
		return err
	}

	collected, err := source.Collect(ctx, source.Options{
		PR:       opts.PR,
		DiffFile: opts.DiffFile,
		Base:     opts.Base,
		RepoDir:  opts.RepoDir,
		Stdin:    env.Stdin,
		Logf:     logf(env.Stderr),
	})
	if err != nil {
		return err
	}
	if len(bytes.TrimSpace(collected.Diff)) == 0 {
		return fmt.Errorf("diff is empty; nothing to review")
	}
	files, err := diff.Parse(string(collected.Diff))
	if err != nil {
		return fmt.Errorf("parse diff: %w", err)
	}
	if len(files) == 0 {
		return fmt.Errorf("could not parse any files from the diff")
	}
	logf(env.Stderr)("parsed %d changed file(s)", len(files))

	selection, err := provider.Select(provider.SelectOptions{
		Config:        loadedConfig.Config,
		RepoDir:       repoRoot,
		ModelOverride: opts.Model,
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(env.Stderr, "  provider: %s / model: %s\n", selection.Provider.Name(), selection.Model)

	if err := guard.Confirm(guard.Plan{
		Calls: 1, Provider: selection.Provider.Name(), Model: selection.Model,
	}, opts.Yes, env.Stdin, env.Stderr, isTTY(env.Stdin)); err != nil {
		return err
	}

	cacheStore, err := cache.Default()
	if err != nil {
		return err
	}
	cacheKey := cache.Key(collected.Diff, selection.Provider.Name(), selection.Model, document.SchemaVersion)
	var result document.Document
	if !opts.NoCache {
		if cached, ok := cacheStore.Load(cacheKey); ok {
			logf(env.Stderr)("cache hit")
			cached.Source = collected.Source
			cached.Files = files
			result = cached
		}
	}
	if result.SchemaVersion == 0 {
		analysis, analysisErr := analyze.Run(ctx, analyze.Options{
			Provider: selection.Provider,
			Source:   collected.Source,
			Files:    files,
			Logf:     logf(env.Stderr),
		})
		if analysisErr != nil {
			return analysisErr
		}
		result = document.Document{
			SchemaVersion: document.SchemaVersion,
			Source:        collected.Source,
			Files:         files,
			Analysis:      analysis,
			Meta: document.Meta{
				Provider:  selection.Provider.Name(),
				Model:     selection.Model,
				Generator: document.Generator,
				Cached:    false,
			},
		}
		if !opts.NoCache {
			if err := cacheStore.Store(cacheKey, result); err != nil {
				return fmt.Errorf("write cache: %w", err)
			}
		}
	}
	logf(env.Stderr)("organized into %d cohort(s)", len(result.Analysis.Cohorts))

	outputPath := opts.Out
	if outputPath == "" {
		outputPath = "walkthrough-" + collected.Source.Name + ".json"
	}
	outputPath, err = filepath.Abs(outputPath)
	if err != nil {
		return err
	}
	if err := writeDocument(outputPath, result); err != nil {
		return err
	}
	fmt.Fprintf(env.Stderr, "\n  wrote %s\n", outputPath)
	return nil
}

func parseArgs(args []string, env Environment) (options, error) {
	cwd, err := env.Getwd()
	if err != nil {
		return options{}, err
	}
	var result options
	if len(args) > 0 && isPRNumber(args[0]) {
		result.PR = args[0]
		args = args[1:]
	}
	flags := flag.NewFlagSet("better-git-review", flag.ContinueOnError)
	flags.SetOutput(env.Stderr)
	flags.Usage = func() {
		fmt.Fprint(env.Stderr, usage)
	}
	flags.StringVar(&result.DiffFile, "diff", "", "unified diff file or - for stdin")
	flags.StringVar(&result.Base, "base", "", "base git ref")
	flags.StringVar(&result.RepoDir, "C", cwd, "repository directory")
	flags.StringVar(&result.Provider, "provider", "", "analysis provider")
	flags.StringVar(&result.Model, "model", "", "provider model")
	flags.StringVar(&result.Out, "out", "", "output JSON path")
	flags.BoolVar(&result.NoCache, "no-cache", false, "bypass analysis cache")
	flags.BoolVar(&result.Yes, "yes", false, "skip confirmations")
	flags.BoolVar(&result.TrustRepoConfig, "trust-repo-config", false, "trust current repo provider config")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return options{}, err
		}
		return options{}, err
	}
	positionals := flags.Args()
	if len(positionals) > 1 {
		return options{}, fmt.Errorf("expected at most one PR number")
	}
	if len(positionals) == 1 {
		if result.PR != "" {
			return options{}, fmt.Errorf("expected at most one PR number")
		}
		if !isPRNumber(positionals[0]) {
			return options{}, fmt.Errorf("invalid PR number %q", positionals[0])
		}
		result.PR = positionals[0]
	}
	if result.PR != "" && result.DiffFile != "" {
		return options{}, fmt.Errorf("PR_NUMBER and --diff cannot be used together")
	}
	result.RepoDir, err = filepath.Abs(result.RepoDir)
	if err != nil {
		return options{}, err
	}
	return result, nil
}

func isPRNumber(value string) bool {
	number, err := strconv.Atoi(value)
	return err == nil && number >= 0 && !strings.HasPrefix(value, "-")
}

func writeDocument(path string, value document.Document) error {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".better-git-review-*.json")
	if err != nil {
		return fmt.Errorf("create output %q: %w", path, err)
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if _, err := temp.Write(append(encoded, '\n')); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempName, path); err != nil {
		return fmt.Errorf("write output %q: %w", path, err)
	}
	return nil
}

func fillEnvironment(env Environment) Environment {
	if env.Stdin == nil {
		env.Stdin = os.Stdin
	}
	if env.Stdout == nil {
		env.Stdout = os.Stdout
	}
	if env.Stderr == nil {
		env.Stderr = os.Stderr
	}
	if env.Getwd == nil {
		env.Getwd = os.Getwd
	}
	return env
}

func isTTY(input io.Reader) bool {
	file, ok := input.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func logf(output io.Writer) func(string, ...any) {
	return func(format string, args ...any) {
		fmt.Fprintf(output, "  "+format+"\n", args...)
	}
}

const usage = `better-git-review - turn a diff into a guided review document

USAGE
  better-git-review [PR_NUMBER] [flags]

SOURCES
  PR_NUMBER              GitHub PR via gh
  --diff <file|->        Unified diff file or stdin
  --base <ref>           Diff <ref>...HEAD (auto-detected by default)

FLAGS
  -C <dir>               Repository directory (default: cwd)
  --provider <name>      Force claude-cli, codex-cli, openrouter, or mock
  --model <model>        Model override for the chosen provider
  --out <file>           Output JSON path
  --no-cache             Bypass the analysis cache
  --yes                  Skip interactive confirmations
  --trust-repo-config    Trust the current repo provider config fingerprint
  -h, --help             Show help
`
