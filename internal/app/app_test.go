package app

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/janiorvalle/better-git-review/internal/config"
	"github.com/janiorvalle/better-git-review/internal/document"
	"github.com/janiorvalle/better-git-review/internal/source"
)

func TestParseArgsAllowsPRBeforeFlags(t *testing.T) {
	opts, err := parseArgs([]string{"123", "--provider", "mock", "--out", "result.json"}, Environment{
		Stderr: &bytes.Buffer{},
		Getwd:  func() (string, error) { return t.TempDir(), nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if opts.PR != "123" || opts.Provider != "mock" || opts.Out != "result.json" {
		t.Fatalf("unexpected options: %#v", opts)
	}
	if opts.Format != "html" {
		t.Fatalf("default format = %q, want html", opts.Format)
	}
}

func TestParseArgsRejectsConflictingSources(t *testing.T) {
	tests := [][]string{
		{"123", "--base", "main"},
		{"--diff", "change.patch", "--base", "main"},
		{"123", "--diff", "change.patch"},
		{"123", "--dirty"},
		{"--diff", "change.patch", "--dirty"},
		{"--base", "main", "--dirty"},
		{"--head", "topic", "--dirty"},
		{"--commit", "abc", "--base", "main"},
		{"--commit", "abc", "123"},
		{"-i", "--diff", "change.patch"},
		{"-i", "--base", "main"},
		{"-i", "--head", "topic"},
		{"-i", "--commit", "abc"},
		{"-i", "--dirty"},
	}
	for _, args := range tests {
		if _, err := parseArgs(args, Environment{
			Stderr: &bytes.Buffer{},
			Getwd:  func() (string, error) { return t.TempDir(), nil },
		}); err == nil {
			t.Fatalf("expected conflicting args to fail: %#v", args)
		}
	}
}

func TestParseArgsInteractiveQueryBeforeOtherFlags(t *testing.T) {
	opts, err := parseArgs([]string{"-i", "auth", "--provider", "mock"}, Environment{
		Stderr: &bytes.Buffer{}, Getwd: func() (string, error) { return t.TempDir(), nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if !opts.Interactive || opts.PickerQuery != "auth" || opts.Provider != "mock" {
		t.Fatalf("opts = %#v", opts)
	}
}

func TestBrowserCommandDispatch(t *testing.T) {
	tests := []struct {
		goos string
		path string
		name string
		args []string
		ok   bool
	}{
		{goos: "darwin", path: "review.html", name: "open", args: []string{"review.html"}, ok: true},
		{goos: "linux", path: "review.html", name: "xdg-open", args: []string{"review.html"}, ok: true},
		{
			goos: "windows",
			path: `C:\reviews\change&notes.html`,
			name: "rundll32",
			args: []string{"url.dll,FileProtocolHandler", `C:\reviews\change&notes.html`},
			ok:   true,
		},
		{goos: "plan9", path: "review.html", ok: false},
	}
	for _, test := range tests {
		t.Run(test.goos, func(t *testing.T) {
			name, args, ok := browserCommand(test.goos, test.path)
			if name != test.name || !slices.Equal(args, test.args) || ok != test.ok {
				t.Fatalf("got %q %#v %v", name, args, ok)
			}
		})
	}
}

func TestParseArgsAcceptsViewerFlags(t *testing.T) {
	opts, err := parseArgs([]string{"--format", "json", "--open"}, Environment{
		Stderr: &bytes.Buffer{},
		Getwd:  func() (string, error) { return t.TempDir(), nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if opts.Format != "json" || !opts.Open {
		t.Fatalf("unexpected viewer options: %#v", opts)
	}
}

func TestShouldOpenDecisionMatrix(t *testing.T) {
	falseValue := false
	tests := []struct {
		name string
		opts options
		cfg  config.Config
		tty  bool
		want bool
	}{
		{name: "tty html defaults open", opts: options{Format: "html"}, tty: true, want: true},
		{name: "non tty stays closed", opts: options{Format: "html"}, tty: false},
		{name: "explicit force", opts: options{Format: "html", Open: true}, tty: false, want: true},
		{name: "no open wins", opts: options{Format: "html", NoOpen: true}, tty: true},
		{name: "json never opens", opts: options{Format: "json", Open: true}, tty: true},
		{name: "config disables", opts: options{Format: "html"}, cfg: config.Config{AutoOpen: &falseValue}, tty: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := shouldOpen(test.opts, test.cfg, test.tty); got != test.want {
				t.Fatalf("shouldOpen = %v, want %v", got, test.want)
			}
		})
	}
}

func TestFirstRunOnboardingWritesConfig(t *testing.T) {
	configHome := useTestConfigHome(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	stdinTTY := true
	stderrTTY := false
	input := strings.NewReader("2\n\n\n\n\nn\n")
	if err := Run(context.Background(), nil, Environment{
		Stdin: input, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{},
		Getwd:    func() (string, error) { return t.TempDir(), nil },
		StdinTTY: &stdinTTY, StderrTTY: &stderrTTY,
	}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(configHome, "better-git-review", "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, expected := range []string{`provider = "codex-cli"`, `model = "gpt-5.6-luna"`, `reasoning = "low"`} {
		if !strings.Contains(text, expected) {
			t.Fatalf("config missing %q:\n%s", expected, text)
		}
	}
}

func TestEmptyGitDiffEntersPickerWhenInputIsTTY(t *testing.T) {
	repo := t.TempDir()
	runAppGit(t, repo, "init", "-b", "main")
	runAppGit(t, repo, "config", "user.email", "app@example.com")
	runAppGit(t, repo, "config", "user.name", "App Test")
	if err := os.WriteFile(filepath.Join(repo, "file.txt"), []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runAppGit(t, repo, "add", "file.txt")
	runAppGit(t, repo, "commit", "-m", "first")
	if err := os.WriteFile(filepath.Join(repo, "file.txt"), []byte("two\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runAppGit(t, repo, "add", "file.txt")
	runAppGit(t, repo, "commit", "-m", "second")

	configHome := useTestConfigHome(t)
	useTestStateHome(t)
	if err := config.WriteUser(filepath.Join(configHome, "better-git-review", "config.toml"), config.Config{
		Provider: "mock", Providers: map[string]config.ProviderConfig{},
	}); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "picked.html")
	stdinTTY := true
	stderrTTY := false
	var stderr bytes.Buffer
	err := Run(context.Background(), []string{"--out", output}, Environment{
		Stdin: strings.NewReader("1\n"), Stdout: &bytes.Buffer{}, Stderr: &stderr,
		Getwd: func() (string, error) { return repo, nil }, StdinTTY: &stdinTTY, StderrTTY: &stderrTTY,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr.String(), "Review what?") || !strings.Contains(stderr.String(), "running: bgr --commit") {
		t.Fatalf("picker transcript missing:\n%s", stderr.String())
	}
}

func useTestConfigHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if runtime.GOOS == "windows" {
		t.Setenv("APPDATA", dir)
	} else {
		t.Setenv("XDG_CONFIG_HOME", dir)
	}
	return dir
}

func useTestStateHome(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	if runtime.GOOS == "windows" {
		t.Setenv("LOCALAPPDATA", dir)
	} else {
		t.Setenv("XDG_STATE_HOME", dir)
	}
}

func runAppGit(t *testing.T, repo string, args ...string) {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = repo
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, output)
	}
}

func TestParseArgsRejectsUnknownFormat(t *testing.T) {
	if _, err := parseArgs([]string{"--format", "pdf"}, Environment{
		Stderr: &bytes.Buffer{},
		Getwd:  func() (string, error) { return t.TempDir(), nil },
	}); err == nil {
		t.Fatal("expected unknown format to fail")
	}
}

func TestRunVersionDoesNotRequireARepositoryOrProvider(t *testing.T) {
	var output bytes.Buffer
	if err := Run(context.Background(), []string{"--version"}, Environment{
		Stdout: &output,
		Stderr: &bytes.Buffer{},
		Getwd:  func() (string, error) { return t.TempDir(), nil },
	}); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(output.String()) != document.Generator() {
		t.Fatalf("version output = %q", output.String())
	}
}

func TestParseStageRejectsEmptyAndParsesFiles(t *testing.T) {
	if _, err := parseStage(source.Result{}, func(string, ...any) {}); err == nil {
		t.Fatal("empty diff should fail")
	}
	files, err := parseStage(source.Result{Diff: []byte(
		"diff --git a/a.go b/a.go\n" +
			"--- a/a.go\n" +
			"+++ b/a.go\n" +
			"@@ -1 +1 @@\n" +
			"-package old\n" +
			"+package current\n",
	)}, func(string, ...any) {})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Path != "a.go" {
		t.Fatalf("unexpected files: %#v", files)
	}
}

func TestRenderStageSupportsJSONAndHTML(t *testing.T) {
	value := document.Document{
		SchemaVersion: document.SchemaVersion,
		Source:        document.Source{Title: "Change", Name: "change"},
		Files:         []document.File{{Path: "a.go"}},
		Analysis: document.Analysis{
			Title: "Change", Overview: "Overview", StubbedFiles: []int{},
			Cohorts: []document.Cohort{{
				Title: "Backend", Layer: "backend", Intent: "Intent", Narrative: "Narrative",
				Files: []int{0}, FileSummaries: []string{"Summary"},
				ReviewNotes: []string{}, DependsOn: []int{},
			}},
		},
	}
	jsonOutput, err := renderStage("json", value)
	if err != nil {
		t.Fatal(err)
	}
	var decoded document.Document
	if err := json.Unmarshal(jsonOutput, &decoded); err != nil {
		t.Fatalf("invalid JSON output: %v", err)
	}
	htmlOutput, err := renderStage("html", value)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(htmlOutput), "<!doctype html>") {
		t.Fatal("HTML stage did not render a document")
	}
}

func TestOutputPathUsesSourceName(t *testing.T) {
	path, err := outputPath(options{Format: "html"}, "branch")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "walkthrough-branch.html" {
		t.Fatalf("path = %q", path)
	}
}
