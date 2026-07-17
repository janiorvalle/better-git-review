package config

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMergePrecedence(t *testing.T) {
	autoOpen := true
	disableOpen := false
	user := Config{
		Provider: "claude-cli",
		AutoOpen: &autoOpen,
		Providers: map[string]ProviderConfig{
			"claude-cli": {Model: "haiku"},
			"openrouter": {Model: "user-model", APIKeyEnv: "USER_KEY"},
		},
	}
	repo := Config{
		Provider: "openrouter",
		AutoOpen: &disableOpen,
		Providers: map[string]ProviderConfig{
			"openrouter": {Model: "repo-model", Reasoning: "high", BaseURL: "https://repo.example"},
		},
	}
	got := Merge(user, repo)
	if got.Provider != "openrouter" {
		t.Fatalf("provider = %q", got.Provider)
	}
	openrouter := got.Providers["openrouter"]
	if openrouter.Model != "repo-model" || openrouter.APIKeyEnv != "USER_KEY" ||
		openrouter.Reasoning != "high" || openrouter.BaseURL != "https://repo.example" {
		t.Fatalf("unexpected merged provider: %#v", openrouter)
	}
	if got.AutoOpen == nil || *got.AutoOpen {
		t.Fatalf("auto_open was not overridden: %#v", got.AutoOpen)
	}
}

func TestLoadFlagsOverrideRepoAndUser(t *testing.T) {
	temp := t.TempDir()
	userPath := filepath.Join(temp, "user.toml")
	repoPath := filepath.Join(temp, "repo.toml")
	trustPath := filepath.Join(temp, "trust.toml")
	if err := os.WriteFile(userPath, []byte("provider = \"claude-cli\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(repoPath, []byte("provider = \"openrouter\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(LoadOptions{
		RepoDir: temp, UserConfigPath: userPath, RepoConfigPath: repoPath, TrustPath: trustPath,
		Flags: Flags{Provider: "mock"}, AcceptRepoTrust: true,
		Input: bytes.NewBuffer(nil), Output: &bytes.Buffer{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Config.Provider != "mock" {
		t.Fatalf("provider = %q, want mock", loaded.Config.Provider)
	}
}

func TestFingerprintStableAndChanges(t *testing.T) {
	first := Config{
		Provider: "openrouter",
		Providers: map[string]ProviderConfig{
			"z": {Model: "z"},
			"a": {Model: "a"},
		},
	}
	second := Config{
		Provider: "openrouter",
		Providers: map[string]ProviderConfig{
			"a": {Model: "a"},
			"z": {Model: "z"},
		},
	}
	firstHash, _ := Fingerprint(first)
	secondHash, _ := Fingerprint(second)
	if firstHash != secondHash {
		t.Fatalf("map order changed fingerprint: %s != %s", firstHash, secondHash)
	}
	second.Providers["a"] = ProviderConfig{Model: "changed"}
	changedHash, _ := Fingerprint(second)
	if changedHash == firstHash {
		t.Fatal("provider change did not change fingerprint")
	}
	second = first
	second.Providers = cloneProviders(first.Providers)
	value := second.Providers["a"]
	value.Reasoning = "high"
	second.Providers["a"] = value
	reasoningHash, _ := Fingerprint(second)
	if reasoningHash == firstHash {
		t.Fatal("reasoning change did not change fingerprint")
	}
	value = second.Providers["a"]
	value.AnalysisBudget = 900_000
	second.Providers["a"] = value
	budgetHash, _ := Fingerprint(second)
	if budgetHash == reasoningHash {
		t.Fatal("analysis budget change did not change fingerprint")
	}
}

func TestMergeAnalysisBudgetOverride(t *testing.T) {
	got := Merge(
		Config{Providers: map[string]ProviderConfig{"mock": {AnalysisBudget: 400_000}}},
		Config{Providers: map[string]ProviderConfig{"mock": {AnalysisBudget: 800_000}}},
	)
	if got.Providers["mock"].AnalysisBudget != 800_000 {
		t.Fatalf("analysis budget = %d", got.Providers["mock"].AnalysisBudget)
	}
}

func TestMergeIncludeMechanicalUsesExplicitOverride(t *testing.T) {
	enabled, disabled := true, false
	got := Merge(Config{IncludeMechanical: &enabled}, Config{IncludeMechanical: &disabled})
	if got.IncludeMechanical == nil || *got.IncludeMechanical {
		t.Fatalf("include_mechanical override = %#v", got.IncludeMechanical)
	}
}

func TestDescribeProviderSettingsEscapesControlCharacters(t *testing.T) {
	description := DescribeProviderSettings(Config{
		Providers: map[string]ProviderConfig{
			"evil\x1b[2J": {BaseURL: "https://example.invalid"},
		},
	})
	if strings.Contains(description, "\x1b") {
		t.Fatalf("description contains a raw escape character: %q", description)
	}
	if !strings.Contains(description, `\x1b`) {
		t.Fatalf("description did not visibly escape the provider name: %q", description)
	}
}

func TestTrustChangeDetection(t *testing.T) {
	temp := t.TempDir()
	repoPath := filepath.Join(temp, "repo.toml")
	trustPath := filepath.Join(temp, "trust.toml")
	if err := os.WriteFile(repoPath, []byte("provider = \"mock\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	base := LoadOptions{
		RepoDir: temp, UserConfigPath: filepath.Join(temp, "missing.toml"),
		RepoConfigPath: repoPath, TrustPath: trustPath, Input: bytes.NewBuffer(nil), Output: &bytes.Buffer{},
	}
	if _, err := Load(base); err == nil {
		t.Fatal("untrusted non-TTY config should be refused")
	}
	base.AcceptRepoTrust = true
	if _, err := Load(base); err != nil {
		t.Fatalf("trust acceptance failed: %v", err)
	}
	base.AcceptRepoTrust = false
	if _, err := Load(base); err != nil {
		t.Fatalf("stored trust was not reused: %v", err)
	}
	if err := os.WriteFile(repoPath, []byte("provider = \"openrouter\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(base); err == nil {
		t.Fatal("changed repo config should require trust again")
	}
}

func TestValidationNamesEveryInvalidKey(t *testing.T) {
	tests := []struct {
		key string
		bad func(*Config)
	}{
		{"analysis.summary_batch_max_files", func(c *Config) { c.Analysis.SummaryBatchMaxFiles = 0 }},
		{"analysis.stage_concurrency", func(c *Config) { c.Analysis.StageConcurrency = -1 }},
		{"analysis.staging_max_files", func(c *Config) { c.Analysis.StagingMaxFiles = 9 }},
		{"viewer.fold_threshold", func(c *Config) { c.Viewer.FoldThreshold = 0 }},
		{"viewer.word_diff_min_similarity", func(c *Config) { c.Viewer.WordDiffMinSimilarity = 1.1 }},
		{"media.max_preview_bytes", func(c *Config) { c.Media.MaxPreviewBytes = 0 }},
		{"media.image_extensions", func(c *Config) { c.Media.ImageExtensions = []string{".avif"} }},
		{"git.context_lines", func(c *Config) { c.Git.ContextLines = -1 }},
		{"git.find_renames", func(c *Config) { c.Git.FindRenames = 101 }},
		{"github.list_limit", func(c *Config) { c.GitHub.ListLimit = 0 }},
		{"network.provider_exec_timeout_seconds", func(c *Config) { c.Network.ProviderExecTimeoutSeconds = 0 }},
		{"cache.max_entries", func(c *Config) { c.Cache.MaxEntries = -1 }},
	}
	for _, test := range tests {
		t.Run(test.key, func(t *testing.T) {
			cfg := Defaults()
			test.bad(&cfg)
			err := Validate(cfg)
			if err == nil || !strings.Contains(err.Error(), test.key) {
				t.Fatalf("error = %v, want offending key %q", err, test.key)
			}
		})
	}
}

func TestRepoConfigOnlyAppliesViewerMediaAndAutoOpen(t *testing.T) {
	temp := t.TempDir()
	repoPath := filepath.Join(temp, "repo.toml")
	if err := os.WriteFile(repoPath, []byte(`
auto_open = false
[viewer]
fold_threshold = 22
word_diff_min_similarity = 0
[media]
max_preview_bytes = 99
[analysis]
summary_batch_max_files = 7
[cache]
max_entries = 2
`), 0o600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	loaded, err := Load(LoadOptions{
		RepoDir: temp, UserConfigPath: filepath.Join(temp, "missing"), RepoConfigPath: repoPath,
		TrustPath: filepath.Join(temp, "trust"), Input: bytes.NewBuffer(nil), Output: &output,
	})
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Config.Viewer.FoldThreshold != 22 || loaded.Config.Viewer.WordDiffMinSimilarity != 0 ||
		loaded.Config.Media.MaxPreviewBytes != 99 || loaded.Config.AutoOpen == nil || *loaded.Config.AutoOpen {
		t.Fatalf("allowed repo settings not applied: %#v", loaded.Config)
	}
	if loaded.Config.Analysis.SummaryBatchMaxFiles != 25 || loaded.Config.Cache.MaxEntries != 200 {
		t.Fatalf("restricted settings applied: %#v", loaded.Config)
	}
	for _, key := range []string{"[analysis]", "[cache]"} {
		if !strings.Contains(output.String(), key) {
			t.Fatalf("missing warning for %s: %s", key, output.String())
		}
	}
}

func TestAnalysisFingerprintIncludesAnalysisAndGitOnly(t *testing.T) {
	base := Defaults()
	original, _ := AnalysisFingerprint(base)
	analysis := base
	analysis.Analysis.SummaryBatchMaxFiles++
	changed, _ := AnalysisFingerprint(analysis)
	if changed == original {
		t.Fatal("analysis change did not affect fingerprint")
	}
	git := base
	git.Git.ContextLines = 4
	changed, _ = AnalysisFingerprint(git)
	if changed == original {
		t.Fatal("git change did not affect fingerprint")
	}
	viewer := base
	viewer.Viewer.FoldThreshold++
	viewer.Media.MaxPreviewBytes++
	unchanged, _ := AnalysisFingerprint(viewer)
	if unchanged != original {
		t.Fatal("viewer/media change affected analysis fingerprint")
	}
}

func TestRepoMediaLimitsCanTightenButNotRaiseUserLimits(t *testing.T) {
	temp := t.TempDir()
	userPath := filepath.Join(temp, "user.toml")
	repoPath := filepath.Join(temp, "repo.toml")
	if err := os.WriteFile(userPath, []byte("[media]\nmax_preview_bytes = 100\nmax_total_preview_bytes = 1000\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(repoPath, []byte("[media]\nmax_preview_bytes = 9999\nmax_total_preview_bytes = 10\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(LoadOptions{
		RepoDir: temp, UserConfigPath: userPath, RepoConfigPath: repoPath,
		TrustPath: filepath.Join(temp, "trust"), Output: &bytes.Buffer{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Config.Media.MaxPreviewBytes != 100 || loaded.Config.Media.MaxTotalPreviewBytes != 10 {
		t.Fatalf("repo media limits = %#v", loaded.Config.Media)
	}
}

func TestValidationRejectsUnknownThemesAndImpossibleFoldContext(t *testing.T) {
	cfg := Defaults()
	cfg.Viewer.ThemeLight = "not-a-theme"
	if err := Validate(cfg); err == nil || !strings.Contains(err.Error(), "viewer.theme_light") {
		t.Fatalf("theme error = %v", err)
	}
	cfg = Defaults()
	cfg.Viewer.FoldContext = 6
	if err := Validate(cfg); err == nil || !strings.Contains(err.Error(), "viewer.fold_context") {
		t.Fatalf("fold error = %v", err)
	}
	cfg = Defaults()
	cfg.Viewer.FoldContext = int(^uint(0) >> 1)
	if err := Validate(cfg); err == nil || !strings.Contains(err.Error(), "viewer.fold_context") {
		t.Fatalf("overflow-safe fold error = %v", err)
	}
}
