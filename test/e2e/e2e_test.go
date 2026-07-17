package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/janiorvalle/better-git-review/internal/cache"
	"github.com/janiorvalle/better-git-review/internal/document"
)

var binaryPath string

func TestMain(m *testing.M) {
	temp, err := os.MkdirTemp("", "better-git-review-e2e-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	binaryName := "bgr"
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
	}
	binaryPath = filepath.Join(temp, binaryName)
	command := exec.Command("go", "build", "-o", binaryPath, "../../cmd/bgr")
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		_ = os.RemoveAll(temp)
		os.Exit(1)
	}
	status := m.Run()
	_ = os.RemoveAll(temp)
	os.Exit(status)
}

func TestHappyPath(t *testing.T) {
	env := isolatedEnvironment(t)
	output := filepath.Join(t.TempDir(), "out.json")
	result := runCLI(t, env, nil, "--diff", fixturePath(t), "--provider", "mock", "--format", "json", "--out", output)
	if result.err != nil {
		t.Fatalf("command failed: %v\n%s", result.err, result.stderr)
	}
	doc := readAndValidate(t, output)
	if doc.Meta.Cached {
		t.Fatal("first run should not be cached")
	}
	if doc.Meta.Provider != "mock" || doc.SchemaVersion != document.SchemaVersion {
		t.Fatalf("unexpected metadata: %#v", doc.Meta)
	}
	if doc.Meta.Staged {
		t.Fatal("small diff unexpectedly used staged analysis")
	}
}

func TestHTMLHappyPathAndSelfContainment(t *testing.T) {
	env := isolatedEnvironment(t)
	output := filepath.Join(t.TempDir(), "out.html")
	result := runCLI(t, env, nil,
		"--diff", viewerFixturePath(t), "--provider", "mock", "--out", output)
	if result.err != nil {
		t.Fatalf("command failed: %v\n%s", result.err, result.stderr)
	}
	html, doc := readHTMLDocument(t, output)
	for _, expected := range []string{
		`id="walkthrough-data"`,
		`class="step-nav-button`,
		`class="viewed-switch`,
		`class="view-unified`,
		`class="view-split`,
		`class="fold-button`,
		`data-view-target="unified"`,
		`data-view-target="split"`,
	} {
		if !strings.Contains(html, expected) {
			t.Fatalf("HTML missing %q", expected)
		}
	}
	if strings.Count(html, `<section class="step`) != len(doc.Analysis.Cohorts)+1 {
		t.Fatalf("HTML does not contain every cohort section")
	}
	assertSelfContained(t, html)
}

func TestHTMLHostilePatchIsInert(t *testing.T) {
	output := filepath.Join(t.TempDir(), "hostile.html")
	result := runCLI(t, isolatedEnvironment(t), nil,
		"--diff", hostileFixturePath(t), "--provider", "mock", "--out", output)
	if result.err != nil {
		t.Fatalf("command failed: %v\n%s", result.err, result.stderr)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	html := string(data)
	for _, unsafe := range []string{
		`</script><script>alert(1)</script>`,
		`<img src=x onerror=alert(2)>`,
	} {
		if strings.Contains(html, unsafe) {
			t.Fatalf("hostile payload reached HTML: %s", unsafe)
		}
	}
	readHTMLDocument(t, output)
}

func TestFormatJSON(t *testing.T) {
	output := filepath.Join(t.TempDir(), "out.json")
	result := runCLI(t, isolatedEnvironment(t), nil,
		"--diff", fixturePath(t), "--provider", "mock", "--format", "json", "--out", output)
	if result.err != nil {
		t.Fatalf("command failed: %v\n%s", result.err, result.stderr)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.HasPrefix(bytes.TrimSpace(data), []byte("<!doctype")) {
		t.Fatal("--format json wrote HTML")
	}
	doc := readAndValidate(t, output)
	if doc.SchemaVersion != document.SchemaVersion {
		t.Fatalf("schema version = %d", doc.SchemaVersion)
	}
	if doc.Meta.Staged || doc.Analysis.StubbedFiles == nil || len(doc.Analysis.StubbedFiles) != 0 {
		t.Fatalf("unexpected single-pass staging metadata: %#v %#v", doc.Meta, doc.Analysis.StubbedFiles)
	}
}

func TestStagedHappyPath(t *testing.T) {
	env := setEnv(isolatedEnvironment(t), "BGR_STAGE_BUDGET", "1")
	output := filepath.Join(t.TempDir(), "staged.html")
	result := runCLI(t, env, nil,
		"--diff", fixturePath(t), "--provider", "mock", "--out", output)
	if result.err != nil {
		t.Fatalf("staged command failed: %v\n%s", result.err, result.stderr)
	}
	html, doc := readHTMLDocument(t, output)
	if !doc.Meta.Staged || len(doc.Analysis.StubbedFiles) != 0 {
		t.Fatalf("unexpected staged metadata: %#v %#v", doc.Meta, doc.Analysis.StubbedFiles)
	}
	for _, file := range doc.Files {
		if !strings.Contains(html, file.Path) {
			t.Fatalf("HTML omitted %q", file.Path)
		}
	}
}

func TestStagedStubDegradation(t *testing.T) {
	env := setEnv(isolatedEnvironment(t), "BGR_STAGE_BUDGET", "1")
	env = setEnv(env, "BGR_MOCK_FAIL_SUMMARY", "service_test.go")
	output := filepath.Join(t.TempDir(), "stubbed.html")
	result := runCLI(t, env, nil,
		"--diff", fixturePath(t), "--provider", "mock", "--out", output)
	if result.err != nil {
		t.Fatalf("stubbed command failed: %v\n%s", result.err, result.stderr)
	}
	html, doc := readHTMLDocument(t, output)
	if len(doc.Analysis.StubbedFiles) != 1 ||
		!slices.ContainsFunc(doc.Analysis.StubbedFiles, func(index int) bool {
			return doc.Files[index].Path == "internal/service_test.go"
		}) {
		t.Fatalf("unexpected stubbed files: %#v", doc.Analysis.StubbedFiles)
	}
	if !strings.Contains(html, "No model summary - grouped from path only.") {
		t.Fatal("HTML did not visibly flag the stubbed file")
	}
}

func TestStagedCostGuard(t *testing.T) {
	env := isolatedEnvironment(t)
	promptLog := filepath.Join(t.TempDir(), "calls.log")
	env = setEnv(env, "BGR_MOCK_PROMPT_LOG", promptLog)
	output := filepath.Join(t.TempDir(), "guard.json")
	args := []string{
		"--diff", manyFilesFixturePath(t, 151), "--provider", "mock",
		"--format", "json", "--out", output,
	}
	refused := runCLI(t, env, nil, args...)
	assertFailureContains(t, refused, "fixed plan has exactly 10 model calls")
	assertFailureContains(t, refused, "--yes")

	approved := runCLI(t, env, nil, append(args, "--yes")...)
	if approved.err != nil {
		t.Fatalf("--yes staged command failed: %v\n%s", approved.err, approved.stderr)
	}
	if doc := readAndValidate(t, output); !doc.Meta.Staged {
		t.Fatal("approved run was not staged")
	}
	log, err := os.ReadFile(promptLog)
	if err != nil {
		t.Fatal(err)
	}
	if calls := strings.Count(string(log), "----- MOCK PROMPT -----"); calls != 10 {
		t.Fatalf("actual calls = %d, guard planned 10", calls)
	}
}

func TestStagedMechanicalProvenanceIsNotStubbed(t *testing.T) {
	patch := filepath.Join(t.TempDir(), "mechanical.patch")
	data := `diff --git a/image.png b/image.png
index 1111111..2222222 100644
Binary files a/image.png and b/image.png differ
diff --git a/src/app.go b/src/app.go
index 1111111..2222222 100644
--- a/src/app.go
+++ b/src/app.go
@@ -1 +1 @@
-package old
+package current
`
	if err := os.WriteFile(patch, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	env := setEnv(isolatedEnvironment(t), "BGR_STAGE_BUDGET", "1")
	promptLog := filepath.Join(t.TempDir(), "calls.log")
	env = setEnv(env, "BGR_MOCK_PROMPT_LOG", promptLog)
	output := filepath.Join(t.TempDir(), "mechanical.html")
	result := runCLI(t, env, nil,
		"--diff", patch, "--provider", "mock", "--out", output)
	if result.err != nil {
		t.Fatalf("command failed: %v\n%s", result.err, result.stderr)
	}
	html, doc := readHTMLDocument(t, output)
	if !slices.Equal(doc.Analysis.MechanicalFiles, []int{0}) ||
		slices.Contains(doc.Analysis.StubbedFiles, 0) {
		t.Fatalf("provenance = mechanical %#v stubbed %#v",
			doc.Analysis.MechanicalFiles, doc.Analysis.StubbedFiles)
	}
	if !strings.Contains(html, "Generated / auto-summarized") {
		t.Fatal("mechanical file did not render with neutral provenance")
	}
	includedOutput := filepath.Join(t.TempDir(), "included.json")
	included := runCLI(t, env, nil,
		"--diff", patch, "--provider", "mock", "--include-mechanical",
		"--format", "json", "--out", includedOutput)
	if included.err != nil {
		t.Fatalf("include-mechanical command failed: %v\n%s", included.err, included.stderr)
	}
	if doc := readAndValidate(t, includedOutput); len(doc.Analysis.MechanicalFiles) != 0 {
		t.Fatalf("include-mechanical did not override triage: %#v", doc.Analysis.MechanicalFiles)
	}
	configPath := filepath.Join(envConfigHome(env), "better-git-review", "config.toml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("include_mechanical = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	configOutput := filepath.Join(t.TempDir(), "config-included.json")
	configured := runCLI(t, env, nil,
		"--diff", patch, "--provider", "mock", "--no-cache",
		"--format", "json", "--out", configOutput)
	if configured.err != nil {
		t.Fatalf("include_mechanical config command failed: %v\n%s", configured.err, configured.stderr)
	}
	if doc := readAndValidate(t, configOutput); len(doc.Analysis.MechanicalFiles) != 0 {
		t.Fatalf("include_mechanical config did not override triage: %#v", doc.Analysis.MechanicalFiles)
	}
}

func TestDelimiterInjectionIsNeutralized(t *testing.T) {
	env := isolatedEnvironment(t)
	promptLog := filepath.Join(t.TempDir(), "prompt.log")
	env = setEnv(env, "BGR_MOCK_PROMPT_LOG", promptLog)
	output := filepath.Join(t.TempDir(), "delimiter.json")
	result := runCLI(t, env, nil,
		"--diff", delimiterFixturePath(t), "--provider", "mock",
		"--format", "json", "--out", output)
	if result.err != nil {
		t.Fatalf("delimiter command failed: %v\n%s", result.err, result.stderr)
	}
	promptBytes, err := os.ReadFile(promptLog)
	if err != nil {
		t.Fatal(err)
	}
	prompt := string(promptBytes)
	if strings.Contains(prompt, "BEGIN_UNTRUSTED_CHANGE_DATA") ||
		strings.Contains(prompt, "END_UNTRUSTED_CHANGE_DATA") {
		t.Fatalf("legacy delimiter survived prompt assembly:\n%s", prompt)
	}
	beginPattern := regexp.MustCompile(`BEGIN_UNTRUSTED_[0-9a-f]{16}`)
	endPattern := regexp.MustCompile(`END_UNTRUSTED_[0-9a-f]{16}`)
	begin := beginPattern.FindString(prompt)
	end := endPattern.FindString(prompt)
	if begin == "" || end == "" || strings.Count(prompt, begin) != 2 || strings.Count(prompt, end) != 2 {
		t.Fatalf("nonce delimiter framing is invalid:\n%s", prompt)
	}
	if !strings.Contains(prompt, "[neutralized]") {
		t.Fatal("prompt did not contain the neutralized fixture marker")
	}
}

func TestSinglePassRegression(t *testing.T) {
	output := filepath.Join(t.TempDir(), "single.json")
	result := runCLI(t, isolatedEnvironment(t), nil,
		"--diff", fixturePath(t), "--provider", "mock",
		"--format", "json", "--out", output)
	if result.err != nil {
		t.Fatalf("single-pass command failed: %v\n%s", result.err, result.stderr)
	}
	doc := readAndValidate(t, output)
	if doc.Meta.Staged {
		t.Fatal("small diff unexpectedly staged")
	}
}

func TestBlameLocalGitOnly(t *testing.T) {
	repo := initializeRepo(t)
	runGit(t, repo, "config", "user.name", "Alice Base")
	runGit(t, repo, "switch", "-c", "feature")
	if err := os.WriteFile(filepath.Join(repo, "base.txt"), []byte("changed by Bob\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "config", "user.name", "Bob Feature")
	runGit(t, repo, "add", "base.txt")
	runGit(t, repo, "commit", "-m", "feature")

	output := filepath.Join(t.TempDir(), "blame.json")
	result := runCLI(t, isolatedEnvironment(t), nil,
		"-C", repo, "--base", "main", "--provider", "mock", "--format", "json", "--out", output)
	if result.err != nil {
		t.Fatalf("command failed: %v\n%s", result.err, result.stderr)
	}
	doc := readAndValidate(t, output)
	if doc.Files[0].Hunks[0].Blame == nil || doc.Files[0].Hunks[0].Blame.Author != "Bob Feature" {
		t.Fatalf("unexpected local blame: %#v", doc.Files[0].Hunks[0].Blame)
	}

	patchOutput := filepath.Join(t.TempDir(), "patch.json")
	patchRun := runCLI(t, isolatedEnvironment(t), nil,
		"--diff", fixturePath(t), "--provider", "mock", "--format", "json", "--out", patchOutput)
	if patchRun.err != nil {
		t.Fatalf("patch command failed: %v\n%s", patchRun.err, patchRun.stderr)
	}
	patchDoc := readAndValidate(t, patchOutput)
	for _, file := range patchDoc.Files {
		for _, hunk := range file.Hunks {
			if hunk.Blame != nil {
				t.Fatalf("patch mode unexpectedly carried blame: %#v", hunk.Blame)
			}
		}
	}
}

func TestCacheSchemaBumpDoesNotReuseV2(t *testing.T) {
	env := isolatedEnvironment(t)
	stateHome := envValue(env, "XDG_STATE_HOME")
	diffBytes, err := os.ReadFile(fixturePath(t))
	if err != nil {
		t.Fatal(err)
	}
	v2Key := cache.Key(diffBytes, "mock", "deterministic", "", 2)
	cacheDir := filepath.Join(stateHome, "better-git-review", "cache")
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		t.Fatal(err)
	}
	stale := document.Document{SchemaVersion: 2}
	staleBytes, _ := json.Marshal(stale)
	if err := os.WriteFile(filepath.Join(cacheDir, v2Key+".json"), staleBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "out.json")
	result := runCLI(t, env, nil,
		"--diff", fixturePath(t), "--provider", "mock", "--format", "json", "--out", output)
	if result.err != nil {
		t.Fatalf("command failed: %v\n%s", result.err, result.stderr)
	}
	doc := readAndValidate(t, output)
	if doc.Meta.Cached || strings.Contains(result.stderr, "cache hit") {
		t.Fatal("v2 cache entry was reused")
	}
	if cache.Key(diffBytes, "mock", "deterministic", "", 3) == v2Key {
		t.Fatal("schema bump did not change cache key")
	}
}

func TestCache(t *testing.T) {
	env := isolatedEnvironment(t)
	output := filepath.Join(t.TempDir(), "out.json")
	args := []string{"--diff", fixturePath(t), "--provider", "mock", "--format", "json", "--out", output}
	first := runCLI(t, env, nil, args...)
	if first.err != nil {
		t.Fatalf("first command failed: %v\n%s", first.err, first.stderr)
	}
	if readAndValidate(t, output).Meta.Cached {
		t.Fatal("first run should not be cached")
	}
	second := runCLI(t, env, nil, args...)
	if second.err != nil {
		t.Fatalf("second command failed: %v\n%s", second.err, second.stderr)
	}
	if !readAndValidate(t, output).Meta.Cached {
		t.Fatal("second run should be cached")
	}
	if !strings.Contains(second.stderr, "cache hit") {
		t.Fatalf("stderr did not mention cache hit: %s", second.stderr)
	}
	third := runCLI(t, env, nil, append(args, "--no-cache")...)
	if third.err != nil {
		t.Fatalf("no-cache command failed: %v\n%s", third.err, third.stderr)
	}
	if readAndValidate(t, output).Meta.Cached {
		t.Fatal("--no-cache run should not be cached")
	}
}

func TestCacheDoesNotCrossAnalysisStrategies(t *testing.T) {
	baseEnv := isolatedEnvironment(t)
	output := filepath.Join(t.TempDir(), "strategy.json")
	args := []string{
		"--diff", fixturePath(t), "--provider", "mock",
		"--format", "json", "--out", output,
	}
	singleEnv := setEnv(baseEnv, "BGR_STAGE_BUDGET", "999999")
	first := runCLI(t, singleEnv, nil, args...)
	if first.err != nil {
		t.Fatalf("single-pass command failed: %v\n%s", first.err, first.stderr)
	}
	if doc := readAndValidate(t, output); doc.Meta.Staged {
		t.Fatal("first run unexpectedly staged")
	}

	stagedEnv := setEnv(baseEnv, "BGR_STAGE_BUDGET", "1")
	second := runCLI(t, stagedEnv, nil, args...)
	if second.err != nil {
		t.Fatalf("staged command failed: %v\n%s", second.err, second.stderr)
	}
	if doc := readAndValidate(t, output); !doc.Meta.Staged || doc.Meta.Cached {
		t.Fatalf("single-pass cache crossed into staged run: %#v", doc.Meta)
	}
	if strings.Contains(second.stderr, "cache hit") {
		t.Fatal("staged run reused single-pass cache")
	}

	third := runCLI(t, singleEnv, nil, args...)
	if third.err != nil {
		t.Fatalf("second single-pass command failed: %v\n%s", third.err, third.stderr)
	}
	if doc := readAndValidate(t, output); doc.Meta.Staged || !doc.Meta.Cached {
		t.Fatalf("staged cache crossed into single-pass run: %#v", doc.Meta)
	}
}

func TestCacheReusesIdenticalDiffWithCurrentSourceMetadata(t *testing.T) {
	env := isolatedEnvironment(t)
	temp := t.TempDir()
	firstPatch := filepath.Join(temp, "first.patch")
	secondPatch := filepath.Join(temp, "second.patch")
	data, err := os.ReadFile(fixturePath(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(firstPatch, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secondPatch, data, 0o600); err != nil {
		t.Fatal(err)
	}
	firstOutput := filepath.Join(temp, "first.json")
	secondOutput := filepath.Join(temp, "second.json")
	first := runCLI(t, env, nil, "--diff", firstPatch, "--provider", "mock", "--format", "json", "--out", firstOutput)
	if first.err != nil {
		t.Fatalf("first command failed: %v\n%s", first.err, first.stderr)
	}
	second := runCLI(t, env, nil, "--diff", secondPatch, "--provider", "mock", "--format", "json", "--out", secondOutput)
	if second.err != nil {
		t.Fatalf("second command failed: %v\n%s", second.err, second.stderr)
	}
	doc := readAndValidate(t, secondOutput)
	if !doc.Meta.Cached || !strings.Contains(second.stderr, "cache hit") {
		t.Fatalf("identical diff did not reuse cache: %#v\n%s", doc.Meta, second.stderr)
	}
	if doc.Source.Title != "second.patch" {
		t.Fatalf("source title = %q", doc.Source.Title)
	}
	if doc.Source.Range != "second.patch" {
		t.Fatalf("source range = %q", doc.Source.Range)
	}
}

func TestStdin(t *testing.T) {
	env := isolatedEnvironment(t)
	patch, err := os.ReadFile(fixturePath(t))
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "stdin.json")
	result := runCLI(t, env, patch, "--diff", "-", "--provider", "mock", "--format", "json", "--out", output)
	if result.err != nil {
		t.Fatalf("command failed: %v\n%s", result.err, result.stderr)
	}
	readAndValidate(t, output)
}

func TestGitSources(t *testing.T) {
	t.Run("branch diff", func(t *testing.T) {
		repo := initializeRepo(t)
		subdir := filepath.Join(repo, "nested")
		if err := os.Mkdir(subdir, 0o700); err != nil {
			t.Fatal(err)
		}
		runGit(t, repo, "switch", "-c", "feature")
		if err := os.WriteFile(filepath.Join(repo, "feature.go"), []byte("package sample\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		runGit(t, repo, "add", "feature.go")
		runGit(t, repo, "commit", "-m", "feature")

		output := filepath.Join(t.TempDir(), "branch.json")
		result := runCLI(t, isolatedEnvironment(t), nil,
			"-C", subdir, "--base", "main", "--provider", "mock", "--format", "json", "--out", output)
		if result.err != nil {
			t.Fatalf("command failed: %v\n%s", result.err, result.stderr)
		}
		doc := readAndValidate(t, output)
		if len(doc.Files) != 1 || doc.Files[0].Path != "feature.go" {
			t.Fatalf("unexpected files: %#v", doc.Files)
		}
		canonicalRepo, err := filepath.EvalSymlinks(repo)
		if err != nil {
			t.Fatal(err)
		}
		if doc.Source.RepoDir != canonicalRepo {
			t.Fatalf("repoDir = %q, want %q", doc.Source.RepoDir, canonicalRepo)
		}
	})

	t.Run("uncommitted fallback", func(t *testing.T) {
		repo := initializeRepo(t)
		if err := os.WriteFile(filepath.Join(repo, "base.txt"), []byte("changed\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		output := filepath.Join(t.TempDir(), "working-tree.json")
		result := runCLI(t, isolatedEnvironment(t), nil,
			"-C", repo, "--base", "main", "--provider", "mock", "--format", "json", "--out", output)
		if result.err != nil {
			t.Fatalf("command failed: %v\n%s", result.err, result.stderr)
		}
		doc := readAndValidate(t, output)
		if doc.Source.Range != "HEAD (uncommitted)" {
			t.Fatalf("range = %q", doc.Source.Range)
		}
		if len(doc.Files) != 1 || doc.Files[0].Path != "base.txt" {
			t.Fatalf("working-tree change was not collected: %#v", doc.Files)
		}
		if !strings.Contains(result.stderr, "reviewing uncommitted changes instead") {
			t.Fatalf("fallback was not logged: %s", result.stderr)
		}
	})

	t.Run("arbitrary head and single commit", func(t *testing.T) {
		repo := initializeRepo(t)
		runGit(t, repo, "switch", "-c", "one")
		if err := os.WriteFile(filepath.Join(repo, "one.go"), []byte("package one\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		runGit(t, repo, "add", "one.go")
		runGit(t, repo, "commit", "-m", "one commit")
		oneSHA := gitOutput(t, repo, "rev-parse", "HEAD")
		runGit(t, repo, "switch", "main")
		runGit(t, repo, "switch", "-c", "two")
		if err := os.WriteFile(filepath.Join(repo, "two.go"), []byte("package two\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		runGit(t, repo, "add", "two.go")
		runGit(t, repo, "commit", "-m", "two commit")

		headOutput := filepath.Join(t.TempDir(), "head.json")
		headRun := runCLI(t, isolatedEnvironment(t), nil,
			"-C", repo, "--base", "main", "--head", "one", "--provider", "mock", "--format", "json", "--out", headOutput)
		if headRun.err != nil {
			t.Fatalf("head command failed: %v\n%s", headRun.err, headRun.stderr)
		}
		headDoc := readAndValidate(t, headOutput)
		if len(headDoc.Files) != 1 || headDoc.Files[0].Path != "one.go" || headDoc.Source.Range != "main...one" {
			t.Fatalf("unexpected head document: %#v", headDoc)
		}

		commitOutput := filepath.Join(t.TempDir(), "commit.json")
		commitRun := runCLI(t, isolatedEnvironment(t), nil,
			"-C", repo, "--commit", oneSHA, "--provider", "mock", "--format", "json", "--out", commitOutput)
		if commitRun.err != nil {
			t.Fatalf("commit command failed: %v\n%s", commitRun.err, commitRun.stderr)
		}
		commitDoc := readAndValidate(t, commitOutput)
		if len(commitDoc.Files) != 1 || commitDoc.Files[0].Path != "one.go" {
			t.Fatalf("unexpected commit document: %#v", commitDoc)
		}
		if commitDoc.Files[0].Hunks[0].Blame == nil || commitDoc.Files[0].Hunks[0].Blame.Author != "E2E" {
			t.Fatalf("commit blame did not use the reviewed commit: %#v", commitDoc.Files[0].Hunks[0].Blame)
		}
	})
}

func TestInteractivePickerSelectsCommit(t *testing.T) {
	repo := initializeRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "picked.go"), []byte("package picked\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "picked.go")
	runGit(t, repo, "commit", "-m", "picker target")
	sha := gitOutput(t, repo, "rev-parse", "HEAD")
	output := filepath.Join(t.TempDir(), "picker.json")
	result := runCLI(t, isolatedEnvironment(t), []byte("1\n"),
		"-C", repo, "-i", sha, "--provider", "mock", "--format", "json", "--out", output)
	if result.err != nil {
		t.Fatalf("picker failed: %v\n%s", result.err, result.stderr)
	}
	doc := readAndValidate(t, output)
	if len(doc.Files) != 1 || doc.Files[0].Path != "picked.go" || !strings.Contains(result.stderr, "running: bgr --commit") {
		t.Fatalf("unexpected picker result: %#v\n%s", doc.Files, result.stderr)
	}
}

func TestGitHub503FallsBackToLocalRefs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake gh shell shim is covered by Windows unit tests")
	}
	repo := initializeRepo(t)
	remote := filepath.Join(t.TempDir(), "remote.git")
	runGit(t, t.TempDir(), "init", "--bare", remote)
	runGit(t, repo, "remote", "add", "origin", remote)
	runGit(t, repo, "push", "origin", "main")
	baseOID := gitOutput(t, repo, "rev-parse", "main")
	runGit(t, repo, "switch", "-c", "feature")
	if err := os.WriteFile(filepath.Join(repo, "fallback.go"), []byte("package fallback\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "fallback.go")
	runGit(t, repo, "commit", "-m", "fallback")
	headOID := gitOutput(t, repo, "rev-parse", "HEAD")
	runGit(t, repo, "push", "origin", "feature")

	shimDir := t.TempDir()
	shim := filepath.Join(shimDir, "gh")
	metadata := fmt.Sprintf(`{"number":5,"title":"Fallback","body":"","baseRefName":"main","headRefName":"feature","baseRefOid":"%s","headRefOid":"%s","changedFiles":1,"url":"https://example.invalid/5"}`, baseOID, headOID)
	script := "#!/bin/sh\nif [ \"$1 $2\" = \"auth status\" ]; then exit 0; fi\nif [ \"$1 $2\" = \"pr view\" ]; then printf '%s\\n' '" + metadata + "'; exit 0; fi\necho 'HTTP 503 Service Unavailable' >&2\nexit 1\n"
	if err := os.WriteFile(shim, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	env := setEnv(isolatedEnvironment(t), "PATH", shimDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	output := filepath.Join(t.TempDir(), "github.json")
	result := runCLI(t, env, nil, "5", "-C", repo, "--provider", "mock", "--format", "json", "--out", output)
	if result.err != nil {
		t.Fatalf("fallback failed: %v\n%s", result.err, result.stderr)
	}
	doc := readAndValidate(t, output)
	if len(doc.Files) != 1 || doc.Files[0].Path != "fallback.go" || !strings.Contains(result.stderr, "using local git objects instead") {
		t.Fatalf("unexpected fallback: %#v\n%s", doc.Files, result.stderr)
	}
}

func TestPRModeMissingGHIsActionable(t *testing.T) {
	env := setEnv(isolatedEnvironment(t), "PATH", t.TempDir())
	result := runCLI(t, env, nil, "5", "-C", t.TempDir(), "--provider", "mock")
	assertFailureContains(t, result, "cli.github.com")
	assertFailureContains(t, result, "--base/--head")
	assertFailureContains(t, result, "--commit")
}

func TestConfigureRoundTripAndSkillInstall(t *testing.T) {
	env := isolatedEnvironment(t)
	home := envValue(env, "HOME")
	first := runCLI(t, env, []byte("2\n\n\n\n3\n"), "configure")
	if first.err != nil {
		t.Fatalf("configure failed: %v\n%s", first.err, first.stderr)
	}
	configPath := filepath.Join(envConfigHome(env), "better-git-review", "config.toml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`provider = "codex-cli"`, `model = "gpt-5.6-luna"`, `reasoning = "low"`} {
		if !strings.Contains(string(data), expected) {
			t.Fatalf("config missing %q:\n%s", expected, data)
		}
	}
	skillPath := filepath.Join(home, ".codex", "skills", "bgr", "SKILL.md")
	if _, err := os.Stat(skillPath); err != nil {
		t.Fatalf("Codex skill was not installed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "skills", "bgr", "SKILL.md")); !os.IsNotExist(err) {
		t.Fatalf("Claude skill should not be installed: %v", err)
	}

	second := runCLI(t, env, []byte("\n\n\n\n\n"), "configure")
	if second.err != nil {
		t.Fatalf("configure round trip failed: %v\n%s", second.err, second.stderr)
	}
	updated, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, updated) {
		t.Fatalf("round trip changed config:\nBEFORE\n%s\nAFTER\n%s", data, updated)
	}
}

func TestConfigureDefaultSkipsSkillInstall(t *testing.T) {
	env := isolatedEnvironment(t)
	result := runCLI(t, env, []byte("2\n\n\n\n\n"), "configure")
	if result.err != nil {
		t.Fatalf("configure failed: %v\n%s", result.err, result.stderr)
	}
	home := envValue(env, "HOME")
	for _, path := range []string{
		filepath.Join(home, ".claude", "skills", "bgr", "SKILL.md"),
		filepath.Join(home, ".codex", "skills", "bgr", "SKILL.md"),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("default configure installed %s: %v", path, err)
		}
	}
}

func TestFreshNonTTYRunDoesNotOnboard(t *testing.T) {
	env := isolatedEnvironment(t)
	output := filepath.Join(t.TempDir(), "plain.json")
	result := runCLI(t, env, nil, "--diff", fixturePath(t), "--provider", "mock", "--format", "json", "--out", output)
	if result.err != nil {
		t.Fatalf("non-TTY run failed: %v\n%s", result.err, result.stderr)
	}
	if strings.Contains(result.stderr, "Welcome to bgr") || strings.Contains(result.stderr, "bgr configure") {
		t.Fatalf("non-TTY run onboarded:\n%s", result.stderr)
	}
	if strings.Contains(result.stderr, "\x1b") {
		t.Fatalf("non-TTY output contains ANSI bytes: %q", result.stderr)
	}
	if _, err := os.Stat(filepath.Join(envConfigHome(env), "better-git-review", "config.toml")); !os.IsNotExist(err) {
		t.Fatalf("non-TTY run wrote config: %v", err)
	}
}

func TestReasoningFlagReachesProviderAndAnnouncement(t *testing.T) {
	env := isolatedEnvironment(t)
	reasoningLog := filepath.Join(t.TempDir(), "reasoning.txt")
	env = setEnv(env, "BGR_MOCK_REASONING_LOG", reasoningLog)
	output := filepath.Join(t.TempDir(), "reasoning.json")
	result := runCLI(t, env, nil,
		"--diff", fixturePath(t), "--provider", "mock", "--reasoning", "high", "--format", "json", "--out", output)
	if result.err != nil {
		t.Fatalf("reasoning run failed: %v\n%s", result.err, result.stderr)
	}
	readAndValidate(t, output)
	if !strings.Contains(result.stderr, `reasoning: "high"`) {
		t.Fatalf("reasoning announcement missing:\n%s", result.stderr)
	}
	data, err := os.ReadFile(reasoningLog)
	if err != nil || strings.TrimSpace(string(data)) != "high" {
		t.Fatalf("provider reasoning log = %q, %v", data, err)
	}
}

func TestChangedImageEmbedsBeforeAndAfterPreviews(t *testing.T) {
	repo := initializeRepo(t)
	imagePath := filepath.Join(repo, "preview.png")
	writePNG(t, imagePath, 2, 2, color.RGBA{R: 255, A: 255})
	runGit(t, repo, "add", "preview.png")
	runGit(t, repo, "commit", "-m", "add image")
	runGit(t, repo, "branch", "image-base")
	writePNG(t, imagePath, 3, 1, color.RGBA{G: 255, A: 255})
	runGit(t, repo, "add", "preview.png")
	runGit(t, repo, "commit", "-m", "change image")

	output := filepath.Join(t.TempDir(), "image.html")
	result := runCLI(t, isolatedEnvironment(t), nil,
		"-C", repo, "--base", "image-base", "--provider", "mock", "--out", output)
	if result.err != nil {
		t.Fatalf("image review failed: %v\n%s", result.err, result.stderr)
	}
	html, _ := readHTMLDocument(t, output)
	if strings.Count(html, `src="data:image/png;base64,`) != 2 {
		t.Fatalf("expected two embedded PNG previews, got %d", strings.Count(html, `src="data:image/png;base64,`))
	}
	if !strings.Contains(html, "2 x 2 px") || !strings.Contains(html, "3 x 1 px") {
		t.Fatalf("image dimensions missing from preview")
	}
	assertSelfContained(t, html)
}

func TestPatchBinaryImageUsesHonestLabel(t *testing.T) {
	patch := filepath.Join(t.TempDir(), "image.patch")
	data := "diff --git a/image.png b/image.png\nindex 1111111..2222222 100644\nBinary files a/image.png and b/image.png differ\n"
	if err := os.WriteFile(patch, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "patch-image.html")
	result := runCLI(t, isolatedEnvironment(t), nil, "--diff", patch, "--provider", "mock", "--out", output)
	if result.err != nil {
		t.Fatalf("patch image review failed: %v\n%s", result.err, result.stderr)
	}
	html, _ := readHTMLDocument(t, output)
	if !strings.Contains(html, "Binary image \u00b7 content not available from patch input.") || strings.Contains(html, "data:image") {
		t.Fatalf("patch image label is misleading")
	}
}

func TestDirtyReviewsOnlyUncommittedChanges(t *testing.T) {
	repo := initializeRepo(t)
	runGit(t, repo, "switch", "-c", "feature")
	if err := os.WriteFile(filepath.Join(repo, "committed.go"), []byte("package committed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "committed.go")
	runGit(t, repo, "commit", "-m", "committed feature")
	if err := os.WriteFile(filepath.Join(repo, "base.txt"), []byte("dirty\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	output := filepath.Join(t.TempDir(), "dirty.json")
	result := runCLI(t, isolatedEnvironment(t), nil,
		"-C", repo, "--dirty", "--provider", "mock", "--format", "json", "--out", output)
	if result.err != nil {
		t.Fatalf("command failed: %v\n%s", result.err, result.stderr)
	}
	doc := readAndValidate(t, output)
	if doc.Source.Range != "HEAD (uncommitted)" {
		t.Fatalf("range = %q", doc.Source.Range)
	}
	if len(doc.Files) != 1 || doc.Files[0].Path != "base.txt" {
		t.Fatalf("--dirty included committed branch changes: %#v", doc.Files)
	}
}

func TestFailureModes(t *testing.T) {
	t.Run("empty diff", func(t *testing.T) {
		empty := filepath.Join(t.TempDir(), "empty.patch")
		if err := os.WriteFile(empty, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		result := runCLI(t, isolatedEnvironment(t), nil, "--diff", empty, "--provider", "mock")
		assertFailureContains(t, result, "diff is empty")
	})

	t.Run("empty git diff non tty", func(t *testing.T) {
		repo := initializeRepo(t)
		result := runCLI(t, isolatedEnvironment(t), nil,
			"-C", repo, "--provider", "mock", "--out", filepath.Join(t.TempDir(), "empty.html"))
		assertFailureContains(t, result, "diff is empty")
		if strings.Contains(result.stderr, "Review what?") {
			t.Fatalf("non-TTY empty diff opened picker: %s", result.stderr)
		}
	})

	t.Run("no provider", func(t *testing.T) {
		env := isolatedEnvironment(t)
		env = setEnv(env, "PATH", "")
		env = removeEnv(env, "OPENROUTER_API_KEY")
		result := runCLI(t, env, nil, "--diff", fixturePath(t), "--out", filepath.Join(t.TempDir(), "out.json"))
		assertFailureContains(t, result, "claude-cli")
		assertFailureContains(t, result, "codex-cli")
		assertFailureContains(t, result, "openrouter")
	})

	t.Run("unreadable patch", func(t *testing.T) {
		result := runCLI(t, isolatedEnvironment(t), nil,
			"--diff", filepath.Join(t.TempDir(), "missing.patch"), "--provider", "mock")
		assertFailureContains(t, result, "read diff")
	})
}

func TestRepoConfigTrust(t *testing.T) {
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, ".better-git-review.toml"),
		[]byte("provider = \"mock\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	env := isolatedEnvironment(t)
	output := filepath.Join(t.TempDir(), "trusted.json")
	args := []string{"-C", repo, "--diff", fixturePath(t), "--format", "json", "--out", output}

	first := runCLI(t, env, nil, args...)
	assertFailureContains(t, first, "--trust-repo-config")

	second := runCLI(t, env, nil, append(args, "--trust-repo-config")...)
	if second.err != nil {
		t.Fatalf("trust acceptance failed: %v\n%s", second.err, second.stderr)
	}
	readAndValidate(t, output)

	third := runCLI(t, env, nil, args...)
	if third.err != nil {
		t.Fatalf("stored trust was not reused: %v\n%s", third.err, third.stderr)
	}
}

func TestClaudeProvider(t *testing.T) {
	if os.Getenv("BGR_E2E_CLAUDE") != "1" {
		t.Skip("set BGR_E2E_CLAUDE=1 to run the real claude-cli e2e")
	}
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skip("claude executable is not available")
	}
	output := filepath.Join(t.TempDir(), "claude.html")
	ctxEnv := setEnv(os.Environ(), "XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	result := runCLIWithTimeout(t, 5*time.Minute, ctxEnv, nil,
		"--diff", fixturePath(t), "--provider", "claude-cli", "--no-cache", "--out", output)
	if result.err != nil {
		t.Fatalf("claude command failed: %v\n%s", result.err, result.stderr)
	}
	readHTMLDocument(t, output)
}

type commandResult struct {
	stderr string
	err    error
}

func runCLI(t *testing.T, env []string, stdin []byte, args ...string) commandResult {
	t.Helper()
	return runCLIWithTimeout(t, 30*time.Second, env, stdin, args...)
}

func runCLIWithTimeout(t *testing.T, timeout time.Duration, env []string, stdin []byte, args ...string) commandResult {
	t.Helper()
	command := exec.Command(binaryPath, args...)
	command.Env = env
	command.Stdin = bytes.NewReader(stdin)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	var stdout bytes.Buffer
	command.Stdout = &stdout
	timer := time.AfterFunc(timeout, func() {
		if command.Process != nil {
			_ = command.Process.Kill()
		}
	})
	err := command.Run()
	timer.Stop()
	return commandResult{stderr: stderr.String(), err: err}
}

func readAndValidate(t *testing.T, path string) document.Document {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var result document.Document
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	validateDocument(t, result)
	return result
}

func readHTMLDocument(t *testing.T, path string) (string, document.Document) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	html := string(data)
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(html)), "<!doctype html>") ||
		!strings.Contains(html, "<html") || !strings.Contains(html, "</html>") {
		t.Fatal("output is not a complete HTML document")
	}
	startMarker := `<script id="walkthrough-data" type="application/json">`
	start := strings.Index(html, startMarker)
	if start < 0 {
		t.Fatal("JSON island missing")
	}
	start += len(startMarker)
	end := strings.Index(html[start:], "</script>")
	if end < 0 {
		t.Fatal("JSON island closing tag missing")
	}
	var doc document.Document
	if err := json.Unmarshal([]byte(html[start:start+end]), &doc); err != nil {
		t.Fatalf("invalid JSON island: %v", err)
	}
	validateDocument(t, doc)
	return html, doc
}

func validateDocument(t *testing.T, result document.Document) {
	t.Helper()
	if result.SchemaVersion != document.SchemaVersion {
		t.Fatalf("schema version = %d, want %d", result.SchemaVersion, document.SchemaVersion)
	}
	if len(result.Files) == 0 || len(result.Analysis.Cohorts) == 0 {
		t.Fatalf("document is incomplete: %#v", result)
	}
	if result.Analysis.StubbedFiles == nil {
		t.Fatal("stubbedFiles must be present even when empty")
	}
	if result.Analysis.MechanicalFiles == nil {
		t.Fatal("mechanicalFiles must be present even when empty")
	}
	stubbed := map[int]bool{}
	for _, fileIndex := range result.Analysis.StubbedFiles {
		if fileIndex < 0 || fileIndex >= len(result.Files) || stubbed[fileIndex] {
			t.Fatalf("invalid stubbed file index %d", fileIndex)
		}
		stubbed[fileIndex] = true
	}
	mechanical := map[int]bool{}
	for _, fileIndex := range result.Analysis.MechanicalFiles {
		if fileIndex < 0 || fileIndex >= len(result.Files) || mechanical[fileIndex] || stubbed[fileIndex] {
			t.Fatalf("invalid mechanical file index %d", fileIndex)
		}
		mechanical[fileIndex] = true
	}
	seen := make([]int, len(result.Files))
	for cohortIndex, cohort := range result.Analysis.Cohorts {
		if !document.IsLayer(cohort.Layer) {
			t.Fatalf("invalid layer %q", cohort.Layer)
		}
		if len(cohort.Files) != len(cohort.FileSummaries) {
			t.Fatalf("file summaries do not match files: %#v", cohort)
		}
		for _, fileIndex := range cohort.Files {
			if fileIndex < 0 || fileIndex >= len(result.Files) {
				t.Fatalf("out-of-range file index %d", fileIndex)
			}
			seen[fileIndex]++
		}
		for _, dependency := range cohort.DependsOn {
			if dependency < 0 || dependency >= cohortIndex {
				t.Fatalf("invalid dependency %d on cohort %d", dependency, cohortIndex)
			}
		}
	}
	for index, count := range seen {
		if count != 1 {
			t.Fatalf("file %d appears %d times", index, count)
		}
	}
}

func assertSelfContained(t *testing.T, html string) {
	t.Helper()
	// ZERO external references: the walkthrough is a complete offline
	// artifact (mermaid and its CDN were removed in schema v4).
	allowedImageData := regexp.MustCompile(`(?i)<img\b[^>]*\bsrc\s*=\s*["']data:image/(?:png|jpeg|gif|webp|svg(?:\+|&#43;)xml);base64,[a-z0-9+/=&#;]+["'][^>]*>`)
	withoutEmbeddedImages := allowedImageData.ReplaceAllString(html, "")
	attributePattern := regexp.MustCompile(`(?i)(?:src|href)\s*=\s*["']([^"']+)["']`)
	for _, match := range attributePattern.FindAllStringSubmatch(withoutEmbeddedImages, -1) {
		t.Fatalf("unexpected external reference: %s", match[1])
	}
	urlPattern := regexp.MustCompile(`(?i)url\(([^)]+)\)`)
	for _, match := range urlPattern.FindAllStringSubmatch(html, -1) {
		reference := strings.Trim(match[1], `"'`)
		// Same-document fragment references (SVG markers) are not external.
		if !strings.HasPrefix(reference, "#") {
			t.Fatalf("unexpected CSS url reference: %s", match[1])
		}
	}
}

func writePNG(t *testing.T, path string, width, height int, fill color.RGBA) {
	t.Helper()
	value := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			value.Set(x, y, fill)
		}
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(file, value); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func fixturePath(t *testing.T) string {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("testdata", "simple.patch"))
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func viewerFixturePath(t *testing.T) string {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("testdata", "viewer.patch"))
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func hostileFixturePath(t *testing.T) string {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("testdata", "hostile.patch"))
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func stagedFixturePath(t *testing.T) string {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("testdata", "staged.patch"))
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func manyFilesFixturePath(t *testing.T, count int) string {
	t.Helper()
	var patch strings.Builder
	for index := 0; index < count; index++ {
		fmt.Fprintf(&patch, `diff --git a/src/file-%03d.go b/src/file-%03d.go
new file mode 100644
index 0000000..1111111
--- /dev/null
+++ b/src/file-%03d.go
@@ -0,0 +1 @@
+package generated
`, index, index, index)
	}
	path := filepath.Join(t.TempDir(), "many-files.patch")
	if err := os.WriteFile(path, []byte(patch.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func delimiterFixturePath(t *testing.T) string {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("testdata", "delimiter.patch"))
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func isolatedEnvironment(t *testing.T) []string {
	t.Helper()
	root := t.TempDir()
	env := removeEnv(os.Environ(),
		"HOME", "USERPROFILE", "APPDATA", "LOCALAPPDATA", "XDG_CONFIG_HOME", "XDG_STATE_HOME",
		"BGR_STAGE_BUDGET", "BGR_MOCK_FAIL_SUMMARY", "BGR_MOCK_PROMPT_LOG",
		"BGR_MOCK_REASONING_LOG",
	)
	env = append(env,
		"HOME="+root,
		"USERPROFILE="+root,
		"APPDATA="+filepath.Join(root, "appdata"),
		"LOCALAPPDATA="+filepath.Join(root, "localappdata"),
		"XDG_CONFIG_HOME="+filepath.Join(root, "config"),
		"XDG_STATE_HOME="+filepath.Join(root, "state"),
	)
	return env
}

func removeEnv(env []string, names ...string) []string {
	result := make([]string, 0, len(env))
	for _, entry := range env {
		key, _, _ := strings.Cut(entry, "=")
		if slices.Contains(names, key) {
			continue
		}
		result = append(result, entry)
	}
	return result
}

func setEnv(env []string, key, value string) []string {
	env = removeEnv(env, key)
	return append(env, key+"="+value)
}

func envValue(env []string, key string) string {
	prefix := key + "="
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			return strings.TrimPrefix(entry, prefix)
		}
	}
	return ""
}

func envConfigHome(env []string) string {
	if runtime.GOOS == "windows" {
		return envValue(env, "APPDATA")
	}
	return envValue(env, "XDG_CONFIG_HOME")
}

func initializeRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	runGit(t, repo, "init", "-b", "main")
	runGit(t, repo, "config", "user.email", "e2e@example.com")
	runGit(t, repo, "config", "user.name", "E2E")
	runGit(t, repo, "config", "color.ui", "always")
	runGit(t, repo, "config", "diff.mnemonicPrefix", "true")
	if err := os.WriteFile(filepath.Join(repo, "base.txt"), []byte("base\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "base.txt")
	runGit(t, repo, "commit", "-m", "base")
	return repo
}

func runGit(t *testing.T, repo string, args ...string) {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = repo
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, output)
	}
}

func gitOutput(t *testing.T, repo string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = repo
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}

func assertFailureContains(t *testing.T, result commandResult, text string) {
	t.Helper()
	if result.err == nil {
		t.Fatalf("expected command failure; stderr: %s", result.stderr)
	}
	if !strings.Contains(result.stderr, text) {
		t.Fatalf("stderr %q does not contain %q", result.stderr, text)
	}
}
