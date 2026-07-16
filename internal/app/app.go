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
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/janiorvalle/better-git-review/internal/analyze"
	"github.com/janiorvalle/better-git-review/internal/blame"
	"github.com/janiorvalle/better-git-review/internal/cache"
	"github.com/janiorvalle/better-git-review/internal/config"
	"github.com/janiorvalle/better-git-review/internal/diff"
	"github.com/janiorvalle/better-git-review/internal/document"
	"github.com/janiorvalle/better-git-review/internal/guard"
	"github.com/janiorvalle/better-git-review/internal/provider"
	"github.com/janiorvalle/better-git-review/internal/source"
	"github.com/janiorvalle/better-git-review/internal/viewer"
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
	Format          string
	Open            bool
	Dirty           bool
	NoCache         bool
	Yes             bool
	TrustRepoConfig bool
	Version         bool
}

func Run(ctx context.Context, args []string, env Environment) error {
	env = fillEnvironment(env)
	opts, err := parseArgs(args, env)
	if err != nil {
		return err
	}
	if opts.Version {
		fmt.Fprintln(env.Stdout, document.Generator())
		return nil
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

	collected, err := collectStage(ctx, opts, repoRoot, env, defaultSourceRegistry())
	if err != nil {
		return err
	}
	files, err := parseStage(collected, logf(env.Stderr))
	if err != nil {
		return err
	}

	selection, err := selectStage(opts, loadedConfig.Config, env.Stderr, defaultProviderRegistry())
	if err != nil {
		return err
	}

	result, err := analyzeWithCacheStage(ctx, analyzeStageInput{
		Options:   opts,
		Env:       env,
		Collected: collected,
		Files:     files,
		Selection: selection,
		Getenv:    os.Getenv,
	})
	if err != nil {
		return err
	}

	content, err := renderStage(opts.Format, result)
	if err != nil {
		return err
	}
	outputPath, err := outputPath(opts, collected.Source.Name)
	if err != nil {
		return err
	}
	if err := emitStage(outputPath, content); err != nil {
		return err
	}
	fmt.Fprintf(env.Stderr, "\n  wrote %s\n", outputPath)
	if opts.Open && opts.Format == "html" {
		openBrowser(outputPath)
	}
	return nil
}

func collectStage(
	ctx context.Context,
	opts options,
	repoRoot string,
	env Environment,
	registry source.Registry,
) (source.Result, error) {
	return registry.Collect(ctx, source.Options{
		PR:       opts.PR,
		DiffFile: opts.DiffFile,
		Base:     opts.Base,
		Dirty:    opts.Dirty,
		RepoDir:  repoRoot,
		Stdin:    env.Stdin,
		Logf:     logf(env.Stderr),
	})
}

func parseStage(collected source.Result, log func(string, ...any)) ([]document.File, error) {
	if len(bytes.TrimSpace(collected.Diff)) == 0 {
		return nil, fmt.Errorf("diff is empty; nothing to review")
	}
	files, err := diff.Parse(string(collected.Diff))
	if err != nil {
		return nil, fmt.Errorf("parse diff: %w", err)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("could not parse any files from the diff")
	}
	log("parsed %d changed file(s)", len(files))
	return files, nil
}

func selectStage(
	opts options,
	loaded config.Config,
	stderr io.Writer,
	registry provider.Registry,
) (provider.Selection, error) {
	selection, err := registry.Select(provider.SelectOptions{
		Config:        loaded,
		ModelOverride: opts.Model,
	})
	if err != nil {
		return provider.Selection{}, err
	}
	fmt.Fprintf(stderr, "  provider: %q / model: %q\n", selection.Provider.Name(), selection.Model)
	return selection, nil
}

type analyzeStageInput struct {
	Options   options
	Env       Environment
	Collected source.Result
	Files     []document.File
	Selection provider.Selection
	Getenv    func(string) string
}

func analyzeWithCacheStage(ctx context.Context, input analyzeStageInput) (document.Document, error) {
	stageDecision, err := analyze.DecideStaging(input.Files, input.Getenv)
	if err != nil {
		return document.Document{}, err
	}
	if stageDecision.Staged {
		logf(input.Env.Stderr)("analysis input is %d bytes (budget %d); using staged analysis",
			stageDecision.InputBytes, stageDecision.Budget)
	}

	cacheStore, err := cache.Default(validateCachedDocument)
	if err != nil {
		return document.Document{}, err
	}
	cacheKey := cache.Key(
		input.Collected.Diff,
		input.Selection.Provider.Name(),
		input.Selection.Model,
		document.SchemaVersion,
	)
	var result document.Document
	if !input.Options.NoCache {
		if cached, ok := cacheStore.Load(cacheKey); ok && cached.Meta.Staged == stageDecision.Staged {
			logf(input.Env.Stderr)("cache hit")
			cached.Source = input.Collected.Source
			cached.Files = input.Files
			result = cached
		}
	}
	if result.SchemaVersion == 0 {
		if err := guard.Confirm(
			guard.AnalysisPlan(
				len(input.Files),
				stageDecision.Staged,
				input.Selection.Provider.Name(),
				input.Selection.Model,
			),
			input.Options.Yes,
			input.Env.Stdin,
			input.Env.Stderr,
			isTTY(input.Env.Stdin),
		); err != nil {
			return document.Document{}, err
		}
		analysis, analysisErr := analyze.Run(ctx, analyze.Options{
			Provider: input.Selection.Provider,
			Source:   input.Collected.Source,
			Files:    input.Files,
			Logf:     logf(input.Env.Stderr),
			Staged:   stageDecision.Staged,
			Budget:   stageDecision.Budget,
		})
		if analysisErr != nil {
			return document.Document{}, analysisErr
		}
		result = document.Document{
			SchemaVersion: document.SchemaVersion,
			Source:        input.Collected.Source,
			Files:         input.Files,
			Analysis:      analysis,
			Meta: document.Meta{
				Provider:  input.Selection.Provider.Name(),
				Model:     input.Selection.Model,
				Generator: document.Generator(),
				Cached:    false,
				Staged:    stageDecision.Staged,
			},
		}
		if !input.Options.NoCache {
			if err := cacheStore.Store(cacheKey, result); err != nil {
				return document.Document{}, fmt.Errorf("write cache: %w", err)
			}
		}
	}
	logf(input.Env.Stderr)("organized into %d cohort(s)", len(result.Analysis.Cohorts))
	if input.Options.PR == "" && input.Options.DiffFile == "" {
		if result.Source.Range == "HEAD (uncommitted)" {
			blame.EnrichUncommitted(ctx, result.Source.RepoDir, result.Files, nil)
		} else {
			blame.Enrich(ctx, result.Source.RepoDir, result.Files, nil)
		}
	}
	return result, nil
}

func validateCachedDocument(value document.Document) error {
	if validationErrors := analyze.ValidateComplete(value.Analysis, len(value.Files)); len(validationErrors) > 0 {
		return fmt.Errorf("%s", analyze.FormatErrors(validationErrors))
	}
	return nil
}

func renderStage(format string, value document.Document) ([]byte, error) {
	if format == "json" {
		encoded, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			return nil, err
		}
		return append(encoded, '\n'), nil
	}
	return viewer.Render(value)
}

func outputPath(opts options, sourceName string) (string, error) {
	path := opts.Out
	if path == "" {
		path = "walkthrough-" + sourceName + "." + opts.Format
	}
	return filepath.Abs(path)
}

func emitStage(path string, content []byte) error {
	return writeOutput(path, content)
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
	flags.StringVar(&result.Out, "out", "", "output path")
	flags.StringVar(&result.Format, "format", "html", "output format: html or json")
	flags.BoolVar(&result.Open, "open", false, "open generated HTML in the default browser")
	flags.BoolVar(&result.Dirty, "dirty", false, "review only uncommitted changes (git diff HEAD)")
	flags.BoolVar(&result.NoCache, "no-cache", false, "bypass analysis cache")
	flags.BoolVar(&result.Yes, "yes", false, "skip confirmations")
	flags.BoolVar(&result.TrustRepoConfig, "trust-repo-config", false, "trust current repo provider config")
	flags.BoolVar(&result.Version, "version", false, "print version")
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
	if result.Base != "" && (result.PR != "" || result.DiffFile != "") {
		return options{}, fmt.Errorf("--base cannot be used with PR_NUMBER or --diff")
	}
	if result.Dirty && (result.PR != "" || result.DiffFile != "" || result.Base != "") {
		return options{}, fmt.Errorf("--dirty cannot be used with PR_NUMBER, --diff, or --base")
	}
	if result.Format != "html" && result.Format != "json" {
		return options{}, fmt.Errorf("--format must be html or json")
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

func writeOutput(path string, content []byte) error {
	temp, err := os.CreateTemp(filepath.Dir(path), ".better-git-review-*")
	if err != nil {
		return fmt.Errorf("create output %q: %w", path, err)
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if _, err := temp.Write(content); err != nil {
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

func openBrowser(path string) {
	name, args, ok := browserCommand(runtime.GOOS, path)
	if !ok {
		return
	}
	command := exec.Command(name, args...)
	_ = command.Run()
}

func browserCommand(goos, path string) (string, []string, bool) {
	switch goos {
	case "darwin":
		return "open", []string{path}, true
	case "linux":
		return "xdg-open", []string{path}, true
	case "windows":
		return "rundll32", []string{"url.dll,FileProtocolHandler", path}, true
	default:
		return "", nil, false
	}
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

const usage = `bgr - turn a diff into a guided review document

USAGE
  bgr [PR_NUMBER] [flags]

SOURCES
  PR_NUMBER              GitHub PR via gh
  --diff <file|->        Unified diff file or stdin
  --base <ref>           Diff <ref>...HEAD (auto-detected by default)
  --dirty                Review only uncommitted changes (git diff HEAD)

FLAGS
  -C <dir>               Repository directory (default: cwd)
  --provider <name>      Force claude-cli, codex-cli, openrouter, or mock
  --model <model>        Model override for the chosen provider
  --format html|json     Output format (default: html)
  --out <file>           Output path
  --open                 Open generated HTML in the default browser
  --no-cache             Bypass the analysis cache
  --yes                  Skip interactive confirmations
  --trust-repo-config    Trust the current repo provider config fingerprint
  --version              Print version
  -h, --help             Show help
`
