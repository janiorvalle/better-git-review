package app

import (
	"bufio"
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
	configureflow "github.com/janiorvalle/better-git-review/internal/configure"
	"github.com/janiorvalle/better-git-review/internal/diff"
	"github.com/janiorvalle/better-git-review/internal/document"
	"github.com/janiorvalle/better-git-review/internal/gitattr"
	"github.com/janiorvalle/better-git-review/internal/guard"
	"github.com/janiorvalle/better-git-review/internal/media"
	"github.com/janiorvalle/better-git-review/internal/picker"
	"github.com/janiorvalle/better-git-review/internal/provider"
	"github.com/janiorvalle/better-git-review/internal/source"
	"github.com/janiorvalle/better-git-review/internal/terminal"
	"github.com/janiorvalle/better-git-review/internal/viewer"
)

type Environment struct {
	Stdin     io.Reader
	Stdout    io.Writer
	Stderr    io.Writer
	Getwd     func() (string, error)
	StdinTTY  *bool
	StderrTTY *bool
}

type options struct {
	PR                string
	DiffFile          string
	Base              string
	Head              string
	Commit            string
	RepoDir           string
	Provider          string
	Model             string
	Reasoning         string
	Out               string
	Format            string
	Open              bool
	NoOpen            bool
	Dirty             bool
	NoCache           bool
	Yes               bool
	TrustRepoConfig   bool
	IncludeMechanical bool
	Version           bool
	Interactive       bool
	PickerQuery       string
	ArgsPresent       bool
	Configure         bool
}

func Run(ctx context.Context, args []string, env Environment) error {
	env = fillEnvironment(env)
	stdinIsTTY := ttyValue(env.StdinTTY, isTTY(env.Stdin))
	stderrIsTTY := ttyValue(env.StderrTTY, isWriterTTY(env.Stderr))
	if _, ok := env.Stdin.(*bufio.Reader); !ok {
		env.Stdin = bufio.NewReader(env.Stdin)
	}
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
		RepoDir:    "",
		Flags:      config.Flags{Provider: opts.Provider, Model: opts.Model, Reasoning: opts.Reasoning},
		Input:      env.Stdin,
		Output:     env.Stderr,
		InputIsTTY: stdinIsTTY,
	})
	if err != nil {
		return err
	}
	if opts.Configure || (!loadedConfig.UserConfigFound && stdinIsTTY && !opts.ArgsPresent && !opts.Yes) {
		configured, configureErr := configureflow.Run(ctx, configureflow.Options{
			Current: loadedConfig.Config, ConfigPath: loadedConfig.UserConfigPath,
			Registry: defaultProviderRegistry(), Input: env.Stdin, Output: env.Stderr,
			Home: os.Getenv("HOME"), FirstRun: !opts.Configure,
			Styled: stderrIsTTY && os.Getenv("NO_COLOR") == "",
		})
		if configureErr != nil {
			return configureErr
		}
		if opts.Configure || !configured.ReviewNow {
			return nil
		}
		loadedConfig.Config = configured.Config
	} else if !loadedConfig.UserConfigFound && stdinIsTTY && opts.ArgsPresent && !opts.Yes {
		fmt.Fprintln(env.Stderr, "  tip: run `bgr configure` once to save these defaults")
	}
	loadedConfig, err = config.Load(config.LoadOptions{
		RepoDir:         repoRoot,
		Flags:           config.Flags{Provider: opts.Provider, Model: opts.Model, Reasoning: opts.Reasoning},
		AcceptRepoTrust: opts.TrustRepoConfig,
		Yes:             opts.Yes,
		Input:           env.Stdin,
		Output:          env.Stderr,
		InputIsTTY:      stdinIsTTY,
	})
	if err != nil {
		return err
	}
	if loadedConfig.Config.IncludeMechanical != nil && *loadedConfig.Config.IncludeMechanical {
		opts.IncludeMechanical = true
	}
	progress := terminal.New(env.Stderr, stderrIsTTY, os.Getenv("NO_COLOR") != "")

	if opts.Interactive {
		selected, pickErr := picker.Run(ctx, picker.Options{
			RepoDir: repoRoot, Query: opts.PickerQuery, Input: env.Stdin, Output: env.Stderr,
		})
		if errors.Is(pickErr, picker.ErrQuit) {
			return nil
		}
		if pickErr != nil {
			return pickErr
		}
		opts = applyPickerSelection(opts, selected)
	}
	collected, err := collectStage(ctx, opts, repoRoot, env, progress.Logf, defaultSourceRegistry())
	if err != nil {
		return err
	}
	if len(bytes.TrimSpace(collected.Diff)) == 0 && stdinIsTTY && !opts.Interactive && !hasExplicitSource(opts) {
		selected, pickErr := picker.Run(ctx, picker.Options{RepoDir: repoRoot, Input: env.Stdin, Output: env.Stderr})
		if errors.Is(pickErr, picker.ErrQuit) {
			return nil
		}
		if pickErr != nil {
			return pickErr
		}
		opts = applyPickerSelection(opts, selected)
		collected, err = collectStage(ctx, opts, repoRoot, env, progress.Logf, defaultSourceRegistry())
		if err != nil {
			return err
		}
	}
	files, err := parseStage(collected, progress.Logf)
	if err != nil {
		return err
	}

	selection, err := selectStage(opts, loadedConfig.Config, progress, defaultProviderRegistry())
	if err != nil {
		return err
	}

	result, err := analyzeWithCacheStage(ctx, analyzeStageInput{
		Options:    opts,
		Env:        env,
		Collected:  collected,
		Files:      files,
		Selection:  selection,
		Getenv:     os.Getenv,
		InputIsTTY: stdinIsTTY,
		Progress:   progress,
	})
	if err != nil {
		return err
	}

	outputPath, err := outputPath(opts, collected.Source.Name)
	if err != nil {
		return err
	}
	// Parsed hunk text owns its own backing string. Release the source bytes
	// before rendering so monster patch inputs do not remain live alongside
	// the complete HTML artifact.
	collected.Diff = nil
	if err := renderAndEmitStage(ctx, outputPath, opts.Format, result, collected); err != nil {
		return err
	}
	opened := false
	if shouldOpen(opts, loadedConfig.Config, stderrIsTTY) {
		opened = openBrowser(outputPath)
	}
	progress.Wrote(outputPath, opened)
	return nil
}

func collectStage(
	ctx context.Context,
	opts options,
	repoRoot string,
	env Environment,
	log func(string, ...any),
	registry source.Registry,
) (source.Result, error) {
	return registry.Collect(ctx, source.Options{
		PR:       opts.PR,
		DiffFile: opts.DiffFile,
		Base:     opts.Base,
		Head:     opts.Head,
		Commit:   opts.Commit,
		Dirty:    opts.Dirty,
		RepoDir:  repoRoot,
		Stdin:    env.Stdin,
		Logf:     log,
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
	progress *terminal.Progress,
	registry provider.Registry,
) (provider.Selection, error) {
	selection, err := registry.Select(provider.SelectOptions{
		Config:            loaded,
		ModelOverride:     opts.Model,
		ReasoningOverride: opts.Reasoning,
	})
	if err != nil {
		return provider.Selection{}, err
	}
	progress.Provider(selection.Provider.Name(), selection.Model, selection.Reasoning)
	for _, warning := range selection.Warnings {
		progress.Warning(warning)
	}
	return selection, nil
}

type analyzeStageInput struct {
	Options    options
	Env        Environment
	Collected  source.Result
	Files      []document.File
	Selection  provider.Selection
	Getenv     func(string) string
	InputIsTTY bool
	Progress   *terminal.Progress
}

func analyzeWithCacheStage(ctx context.Context, input analyzeStageInput) (document.Document, error) {
	providerBudget := provider.AnalysisBudget(ctx, input.Selection.Provider)
	stageDecision, err := analyze.DecideStaging(input.Files, input.Getenv, providerBudget)
	if err != nil {
		return document.Document{}, err
	}
	plannedCalls := 1
	var stagedPlan *analyze.StagedPlan
	if stageDecision.Staged {
		if len(input.Files) > analyze.CohortMaxFiles && stageDecision.InputBytes <= stageDecision.Budget {
			input.Progress.Logf("big diff - %d files exceeds the %d-file single-pass output limit, using staged analysis",
				len(input.Files), analyze.CohortMaxFiles)
		} else {
			input.Progress.Logf("big diff - roughly %.1fx the %d-char model budget, using staged analysis",
				float64(stageDecision.InputBytes)/float64(stageDecision.Budget), stageDecision.Budget)
		}
		attributeRef := input.Collected.HeadRef
		if attributeRef == "WORKTREE" {
			attributeRef = input.Collected.BaseRef
		}
		generated := gitattr.Generated(
			ctx,
			input.Collected.Source.RepoDir,
			attributeRef,
			input.Files,
			nil,
			input.Progress.Logf,
		)
		plan := analyze.PlanStaged(input.Files, generated, input.Options.IncludeMechanical, stageDecision.Budget)
		stagedPlan = &plan
		plannedCalls = plan.Calls
		input.Progress.Logf(
			"planned %d summary batches + %d cohort narrations + 1 synthesis (%d mechanical files skipped)",
			len(plan.SummaryBatches), len(plan.Cohorts), len(plan.Triage.Mechanical),
		)
	}

	cacheStore, err := cache.Default(validateCachedDocument)
	if err != nil {
		return document.Document{}, err
	}
	cacheKey := cache.Key(
		input.Collected.Diff,
		input.Selection.Provider.Name(),
		input.Selection.Model,
		input.Selection.Reasoning,
		document.SchemaVersion,
		fmt.Sprintf("budget=%d", stageDecision.Budget),
		fmt.Sprintf("include-mechanical=%t", input.Options.IncludeMechanical),
		fmt.Sprintf("mechanical=%v", func() []int {
			if stagedPlan == nil {
				return nil
			}
			return stagedPlan.Triage.Mechanical
		}()),
	)
	var result document.Document
	analysisPerformed := false
	if !input.Options.NoCache {
		if cached, ok := cacheStore.Load(cacheKey); ok && cached.Meta.Staged == stageDecision.Staged {
			input.Progress.Logf("cache hit - reusing the saved analysis, no model calls needed")
			cached.Source = input.Collected.Source
			cached.Files = input.Files
			result = cached
		}
	}
	if result.SchemaVersion == 0 {
		analysisPerformed = true
		if err := guard.Confirm(
			guard.AnalysisPlan(
				plannedCalls,
				input.Selection.Provider.Name(),
				input.Selection.Model,
				input.Selection.Reasoning,
			),
			input.Options.Yes,
			input.Env.Stdin,
			input.Env.Stderr,
			input.InputIsTTY,
		); err != nil {
			return document.Document{}, err
		}
		spinner := input.Progress.Start("analyzing the diff...")
		analysis, analysisErr := analyze.Run(ctx, analyze.Options{
			Provider: input.Selection.Provider,
			Source:   input.Collected.Source,
			Files:    input.Files,
			Logf:     input.Progress.Logf,
			Progress: func(completed, total int) {
				spinner.Update(fmt.Sprintf("summarizing %d/%d ...", completed, total))
			},
			Staged: stageDecision.Staged,
			Budget: stageDecision.Budget,
			Plan:   stagedPlan,
		})
		analysisDuration := spinner.Stop()
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
		if input.Progress.Enabled() {
			input.Progress.Successf("%d cohorts  in %s", len(result.Analysis.Cohorts), terminal.FormatDuration(analysisDuration))
		}
	}
	if input.Progress.Enabled() && !analysisPerformed {
		input.Progress.Successf("%d cohorts  (cached)", len(result.Analysis.Cohorts))
	} else if !input.Progress.Enabled() {
		input.Progress.Successf("grouped into %d review steps", len(result.Analysis.Cohorts))
	}
	if input.Collected.Source.RepoDir != "" {
		if input.Collected.Dirty {
			blame.EnrichUncommitted(ctx, result.Source.RepoDir, result.Files, nil)
		} else {
			blame.EnrichRef(ctx, result.Source.RepoDir, input.Collected.HeadRef, result.Files, nil)
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
	return renderStageWithSource(context.Background(), format, value, source.Result{})
}

func renderStageWithSource(ctx context.Context, format string, value document.Document, collected source.Result) ([]byte, error) {
	if format == "json" {
		encoded, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			return nil, err
		}
		return append(encoded, '\n'), nil
	}
	previews := media.Enrich(ctx, value.Files, media.Source{
		RepoDir: collected.Source.RepoDir, BaseRef: collected.BaseRef,
		HeadRef: collected.HeadRef, Dirty: collected.Dirty,
	}, nil)
	return viewer.RenderWithPreviews(value, previews)
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

func renderAndEmitStage(
	ctx context.Context,
	path, format string,
	value document.Document,
	collected source.Result,
) error {
	if format == "json" {
		content, err := renderStageWithSource(ctx, format, value, collected)
		if err != nil {
			return err
		}
		return emitStage(path, content)
	}
	previews := media.Enrich(ctx, value.Files, media.Source{
		RepoDir: collected.Source.RepoDir, BaseRef: collected.BaseRef,
		HeadRef: collected.HeadRef, Dirty: collected.Dirty,
	}, nil)
	return writeOutputWith(path, func(output io.Writer) error {
		return viewer.RenderToWithPreviews(output, value, previews)
	})
}

func parseArgs(args []string, env Environment) (options, error) {
	cwd, err := env.Getwd()
	if err != nil {
		return options{}, err
	}
	result := options{ArgsPresent: len(args) > 0}
	if len(args) > 0 && args[0] == "configure" {
		result.Configure = true
		args = args[1:]
	}
	args, result.PickerQuery = extractPickerQuery(args)
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
	flags.StringVar(&result.Head, "head", "", "head git ref (paired with --base)")
	flags.StringVar(&result.Commit, "commit", "", "review exactly one commit")
	flags.StringVar(&result.RepoDir, "C", cwd, "repository directory")
	flags.StringVar(&result.Provider, "provider", "", "analysis provider")
	flags.StringVar(&result.Model, "model", "", "provider model")
	flags.StringVar(&result.Reasoning, "reasoning", "", "provider reasoning effort")
	flags.StringVar(&result.Out, "out", "", "output path")
	flags.StringVar(&result.Format, "format", "html", "output format: html or json")
	flags.BoolVar(&result.Open, "open", false, "open generated HTML in the default browser")
	flags.BoolVar(&result.NoOpen, "no-open", false, "do not open generated HTML")
	flags.BoolVar(&result.Dirty, "dirty", false, "review only uncommitted changes (git diff HEAD)")
	flags.BoolVar(&result.NoCache, "no-cache", false, "bypass analysis cache")
	flags.BoolVar(&result.Yes, "yes", false, "skip confirmations")
	flags.BoolVar(&result.TrustRepoConfig, "trust-repo-config", false, "trust current repo provider config")
	flags.BoolVar(&result.IncludeMechanical, "include-mechanical", false, "include generated, binary, and exact-rename files in model analysis")
	flags.BoolVar(&result.Version, "version", false, "print version")
	flags.BoolVar(&result.Interactive, "i", false, "interactively choose what to review")
	flags.BoolVar(&result.Interactive, "interactive", false, "interactively choose what to review")
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
		if result.Interactive {
			result.PickerQuery = positionals[0]
			positionals = nil
		}
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
	if result.Interactive && hasExplicitSource(result) {
		return options{}, fmt.Errorf("-i cannot be used with PR_NUMBER, --diff, --base, --head, --commit, or --dirty")
	}
	if result.Open && result.NoOpen {
		return options{}, fmt.Errorf("--open and --no-open cannot be used together")
	}
	if result.PR != "" && result.DiffFile != "" {
		return options{}, fmt.Errorf("PR_NUMBER and --diff cannot be used together")
	}
	if (result.Base != "" || result.Head != "") && (result.PR != "" || result.DiffFile != "") {
		return options{}, fmt.Errorf("--base/--head cannot be used with PR_NUMBER or --diff")
	}
	if result.Dirty && (result.PR != "" || result.DiffFile != "" || result.Base != "" || result.Head != "" || result.Commit != "") {
		return options{}, fmt.Errorf("--dirty cannot be used with PR_NUMBER, --diff, --base, --head, or --commit")
	}
	if result.Commit != "" && (result.PR != "" || result.DiffFile != "" || result.Base != "" || result.Head != "") {
		return options{}, fmt.Errorf("--commit cannot be used with PR_NUMBER, --diff, --base, or --head")
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

func hasExplicitSource(opts options) bool {
	return opts.PR != "" || opts.DiffFile != "" || opts.Base != "" || opts.Head != "" || opts.Commit != "" || opts.Dirty
}

func extractPickerQuery(args []string) ([]string, string) {
	for index, arg := range args {
		if arg != "-i" && arg != "--interactive" {
			continue
		}
		if index+1 >= len(args) || strings.HasPrefix(args[index+1], "-") {
			return args, ""
		}
		query := args[index+1]
		cleaned := append([]string(nil), args[:index+1]...)
		cleaned = append(cleaned, args[index+2:]...)
		return cleaned, query
	}
	return args, ""
}

func applyPickerSelection(opts options, selected picker.Selection) options {
	opts.PR = selected.PR
	opts.Base = selected.Base
	opts.Head = selected.Head
	opts.Commit = selected.Commit
	opts.Dirty = selected.Dirty
	opts.DiffFile = ""
	return opts
}

func shouldOpen(opts options, cfg config.Config, stderrIsTTY bool) bool {
	if opts.Format != "html" || opts.NoOpen {
		return false
	}
	if opts.Open {
		return true
	}
	if !stderrIsTTY {
		return false
	}
	return cfg.AutoOpen == nil || *cfg.AutoOpen
}

func isPRNumber(value string) bool {
	number, err := strconv.Atoi(value)
	return err == nil && number >= 0 && !strings.HasPrefix(value, "-")
}

func writeOutput(path string, content []byte) error {
	return writeOutputWith(path, func(output io.Writer) error {
		_, err := output.Write(content)
		return err
	})
}

func writeOutputWith(path string, write func(io.Writer) error) error {
	temp, err := os.CreateTemp(filepath.Dir(path), ".better-git-review-*")
	if err != nil {
		return fmt.Errorf("create output %q: %w", path, err)
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if err := write(temp); err != nil {
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

func openBrowser(path string) bool {
	name, args, ok := browserCommand(runtime.GOOS, path)
	if !ok {
		return false
	}
	command := exec.Command(name, args...)
	return command.Run() == nil
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

func isWriterTTY(output io.Writer) bool {
	file, ok := output.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func ttyValue(override *bool, detected bool) bool {
	if override != nil {
		return *override
	}
	return detected
}

const usage = `bgr - turn a diff into a guided review document

USAGE
  bgr [PR_NUMBER] [flags]
	bgr -i [query]
	bgr configure

SOURCES
  PR_NUMBER              GitHub PR via gh
  --diff <file|->        Unified diff file or stdin
  --base <ref>           Diff <ref>...HEAD (auto-detected by default)
	--head <ref>           Diff <base>...<ref>
	--commit <sha>         Review exactly one commit
  --dirty                Review only uncommitted changes (git diff HEAD)
	-i, --interactive      Choose from changes, PRs, branches, and commits

FLAGS
  -C <dir>               Repository directory (default: cwd)
  --provider <name>      Force claude-cli, codex-cli, openrouter, or mock
  --model <model>        Model override for the chosen provider
	--reasoning <level>    Reasoning effort override
  --format html|json     Output format (default: html)
  --out <file>           Output path
	--open                 Force opening generated HTML
	--no-open              Do not open generated HTML in a TTY
  --no-cache             Bypass the analysis cache
  --include-mechanical   Include provably mechanical files in model analysis
  --yes                  Skip interactive confirmations
  --trust-repo-config    Trust the current repo provider config fingerprint
  --version              Print version
  -h, --help             Show help
`
