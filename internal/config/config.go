package config

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/janiorvalle/better-git-review/internal/xdg"
)

type Config struct {
	Provider          string                    `toml:"provider"`
	AutoOpen          *bool                     `toml:"auto_open"`
	IncludeMechanical *bool                     `toml:"include_mechanical"`
	Providers         map[string]ProviderConfig `toml:"providers"`
	Analysis          AnalysisConfig            `toml:"analysis"`
	Viewer            ViewerConfig              `toml:"viewer"`
	Media             MediaConfig               `toml:"media"`
	Git               GitConfig                 `toml:"git"`
	GitHub            GitHubConfig              `toml:"github"`
	Network           NetworkConfig             `toml:"network"`
	Cache             CacheConfig               `toml:"cache"`
	Browser           BrowserConfig             `toml:"browser"`
}

type AnalysisConfig struct {
	SummaryBatchMaxFiles int `toml:"summary_batch_max_files" json:"summaryBatchMaxFiles"`
	StageConcurrency     int `toml:"stage_concurrency" json:"stageConcurrency"`
	DigestMaxFiles       int `toml:"digest_max_files" json:"digestMaxFiles"`
	DigestMaxChars       int `toml:"digest_max_chars" json:"digestMaxChars"`
	FileDiffCap          int `toml:"file_diff_cap" json:"fileDiffCap"`
	GuardCallThreshold   int `toml:"guard_call_threshold" json:"guardCallThreshold"`
	StagingMaxFiles      int `toml:"staging_max_files" json:"stagingMaxFiles"`
	FidelityBudget       int `toml:"fidelity_budget" json:"fidelityBudget"`
}

type ViewerConfig struct {
	CollapseThreshold     int     `toml:"collapse_threshold"`
	FoldThreshold         int     `toml:"fold_threshold"`
	FoldContext           int     `toml:"fold_context"`
	LongLineThreshold     int     `toml:"long_line_threshold"`
	KeySymbolCap          int     `toml:"key_symbol_cap"`
	WordDiffMinSimilarity float64 `toml:"word_diff_min_similarity"`
	ThemeLight            string  `toml:"theme_light"`
	ThemeDark             string  `toml:"theme_dark"`
}

type MediaConfig struct {
	MaxPreviewBytes      int64    `toml:"max_preview_bytes"`
	MaxTotalPreviewBytes int64    `toml:"max_total_preview_bytes"`
	ImageExtensions      []string `toml:"image_extensions"`
}

type GitConfig struct {
	ContextLines int `toml:"context_lines" json:"contextLines"`
	FindRenames  int `toml:"find_renames" json:"findRenames"`
}

type GitHubConfig struct {
	PRDiffMaxFiles int `toml:"pr_diff_max_files"`
	ListLimit      int `toml:"list_limit"`
}

type NetworkConfig struct {
	CatalogTimeoutSeconds      int `toml:"catalog_timeout_seconds"`
	CompletionTimeoutSeconds   int `toml:"completion_timeout_seconds"`
	ProviderExecTimeoutSeconds int `toml:"provider_exec_timeout_seconds"`
}

type CacheConfig struct {
	MaxEntries int `toml:"max_entries"`
}

type BrowserConfig struct {
	Command string `toml:"command"`
}

func Defaults() Config {
	return Config{
		Providers: map[string]ProviderConfig{},
		Analysis: AnalysisConfig{
			SummaryBatchMaxFiles: 25, StageConcurrency: 4, DigestMaxFiles: 40,
			DigestMaxChars: 60_000, FileDiffCap: 12_000, GuardCallThreshold: 5,
			StagingMaxFiles: 150, FidelityBudget: 4_000_000,
		},
		Viewer: ViewerConfig{
			CollapseThreshold: 400, FoldThreshold: 10, FoldContext: 3,
			LongLineThreshold: 4_096, KeySymbolCap: 5, WordDiffMinSimilarity: 0.5,
			ThemeLight: "github", ThemeDark: "github-dark",
		},
		Media: MediaConfig{
			MaxPreviewBytes: 1_572_864, MaxTotalPreviewBytes: 12_582_912,
			ImageExtensions: []string{".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg"},
		},
		GitHub: GitHubConfig{PRDiffMaxFiles: 300, ListLimit: 1_000},
		Network: NetworkConfig{
			CatalogTimeoutSeconds: 10, CompletionTimeoutSeconds: 300,
			ProviderExecTimeoutSeconds: 600,
		},
		Cache: CacheConfig{MaxEntries: 200},
	}
}

type ProviderConfig struct {
	Model          string `toml:"model" json:"model,omitempty"`
	Reasoning      string `toml:"reasoning" json:"reasoning,omitempty"`
	APIKeyEnv      string `toml:"api_key_env" json:"api_key_env,omitempty"`
	BaseURL        string `toml:"base_url" json:"base_url,omitempty"`
	AnalysisBudget int    `toml:"analysis_budget" json:"analysis_budget,omitempty"`
}

type Flags struct {
	Provider  string
	Model     string
	Reasoning string
}

type LoadOptions struct {
	RepoDir         string
	UserConfigPath  string
	RepoConfigPath  string
	TrustPath       string
	Flags           Flags
	AcceptRepoTrust bool
	Yes             bool
	Input           io.Reader
	Output          io.Writer
	InputIsTTY      bool
}

type Loaded struct {
	Config          Config
	UserConfigFound bool
	UserConfigPath  string
	RepoConfig      Config
	RepoConfigFound bool
}

type trustFile struct {
	Repos map[string]string `toml:"repos"`
}

func Load(opts LoadOptions) (Loaded, error) {
	if opts.Input == nil {
		opts.Input = os.Stdin
	}
	if opts.Output == nil {
		opts.Output = os.Stderr
	}
	if opts.UserConfigPath == "" || opts.TrustPath == "" {
		configDir, err := userConfigDir()
		if err != nil {
			return Loaded{}, err
		}
		if opts.UserConfigPath == "" {
			opts.UserConfigPath = filepath.Join(configDir, "better-git-review", "config.toml")
		}
		if opts.TrustPath == "" {
			opts.TrustPath = filepath.Join(configDir, "better-git-review", "trust.toml")
		}
	}
	if opts.RepoConfigPath == "" && opts.RepoDir != "" {
		opts.RepoConfigPath = filepath.Join(opts.RepoDir, ".better-git-review.toml")
	}

	userCfg, userFound, _, err := readConfig(opts.UserConfigPath, Defaults())
	if err != nil {
		return Loaded{}, fmt.Errorf("read user config: %w", err)
	}
	repoCfg, repoFound, repoDefined, err := readConfig(opts.RepoConfigPath, Config{Providers: map[string]ProviderConfig{}})
	if err != nil {
		return Loaded{}, fmt.Errorf("read repo config: %w", err)
	}
	for _, table := range []string{"analysis", "git", "github", "network", "cache", "browser"} {
		if repoDefined[table] {
			fmt.Fprintf(opts.Output, "warning: repo config key [%s] is user-config only; ignoring it\n", table)
		}
	}
	repoCfg.Analysis = AnalysisConfig{}
	repoCfg.Git = GitConfig{}
	repoCfg.GitHub = GitHubConfig{}
	repoCfg.Network = NetworkConfig{}
	repoCfg.Cache = CacheConfig{}
	repoCfg.Browser = BrowserConfig{}
	if repoFound && HasProviderSettings(repoCfg) {
		if err := ensureRepoTrust(opts, repoCfg); err != nil {
			return Loaded{}, err
		}
	}

	merged := Merge(userCfg, repoCfg)
	applyRepoAllowed(&merged, repoCfg, repoDefined, userCfg.Media)
	if err := Validate(merged); err != nil {
		return Loaded{}, err
	}
	if merged.Analysis.StagingMaxFiles > 150 {
		fmt.Fprintf(opts.Output, "warning: analysis.staging_max_files above 150 may break model output caps\n")
	}
	if opts.Flags.Provider != "" {
		merged.Provider = opts.Flags.Provider
	}
	if opts.Flags.Model != "" {
		if merged.Providers == nil {
			merged.Providers = map[string]ProviderConfig{}
		}
		providerName := merged.Provider
		if providerName != "" {
			providerCfg := merged.Providers[providerName]
			providerCfg.Model = opts.Flags.Model
			merged.Providers[providerName] = providerCfg
		}
	}
	if opts.Flags.Reasoning != "" {
		if merged.Providers == nil {
			merged.Providers = map[string]ProviderConfig{}
		}
		providerName := merged.Provider
		if providerName != "" {
			providerCfg := merged.Providers[providerName]
			providerCfg.Reasoning = opts.Flags.Reasoning
			merged.Providers[providerName] = providerCfg
		}
	}
	return Loaded{
		Config: merged, UserConfigFound: userFound, UserConfigPath: opts.UserConfigPath,
		RepoConfig: repoCfg, RepoConfigFound: repoFound,
	}, nil
}

func Merge(base, override Config) Config {
	result := Config{
		Provider:          base.Provider,
		AutoOpen:          cloneBool(base.AutoOpen),
		IncludeMechanical: cloneBool(base.IncludeMechanical),
		Providers:         cloneProviders(base.Providers),
		Analysis:          base.Analysis,
		Viewer:            base.Viewer,
		Media:             cloneMedia(base.Media),
		Git:               base.Git,
		GitHub:            base.GitHub,
		Network:           base.Network,
		Cache:             base.Cache,
		Browser:           base.Browser,
	}
	if override.Provider != "" {
		result.Provider = override.Provider
	}
	if override.AutoOpen != nil {
		result.AutoOpen = cloneBool(override.AutoOpen)
	}
	if override.IncludeMechanical != nil {
		result.IncludeMechanical = cloneBool(override.IncludeMechanical)
	}
	if result.Providers == nil {
		result.Providers = map[string]ProviderConfig{}
	}
	for name, providerOverride := range override.Providers {
		current := result.Providers[name]
		if providerOverride.Model != "" {
			current.Model = providerOverride.Model
		}
		if providerOverride.Reasoning != "" {
			current.Reasoning = providerOverride.Reasoning
		}
		if providerOverride.APIKeyEnv != "" {
			current.APIKeyEnv = providerOverride.APIKeyEnv
		}
		if providerOverride.BaseURL != "" {
			current.BaseURL = providerOverride.BaseURL
		}
		if providerOverride.AnalysisBudget != 0 {
			current.AnalysisBudget = providerOverride.AnalysisBudget
		}
		result.Providers[name] = current
	}
	mergeViewer(&result.Viewer, override.Viewer)
	mergeMedia(&result.Media, override.Media)
	return result
}

func HasProviderSettings(cfg Config) bool {
	return cfg.Provider != "" || cfg.IncludeMechanical != nil || len(cfg.Providers) > 0
}

func Fingerprint(cfg Config) (string, error) {
	type providerEntry struct {
		Name   string         `json:"name"`
		Config ProviderConfig `json:"config"`
	}
	canonical := struct {
		Provider          string          `json:"provider"`
		IncludeMechanical *bool           `json:"includeMechanical,omitempty"`
		Providers         []providerEntry `json:"providers"`
	}{Provider: cfg.Provider, IncludeMechanical: cfg.IncludeMechanical}
	names := make([]string, 0, len(cfg.Providers))
	for name := range cfg.Providers {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		canonical.Providers = append(canonical.Providers, providerEntry{Name: name, Config: cfg.Providers[name]})
	}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func AnalysisFingerprint(cfg Config) (string, error) {
	encoded, err := json.Marshal(struct {
		Analysis AnalysisConfig `json:"analysis"`
		Git      GitConfig      `json:"git"`
	}{cfg.Analysis, cfg.Git})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func DescribeProviderSettings(cfg Config) string {
	var lines []string
	if cfg.Provider != "" {
		lines = append(lines, fmt.Sprintf("provider = %q", cfg.Provider))
	}
	if cfg.IncludeMechanical != nil {
		lines = append(lines, fmt.Sprintf("include_mechanical = %t", *cfg.IncludeMechanical))
	}
	names := make([]string, 0, len(cfg.Providers))
	for name := range cfg.Providers {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		value := cfg.Providers[name]
		lines = append(lines, fmt.Sprintf("[providers.%q]", name))
		if value.Model != "" {
			lines = append(lines, fmt.Sprintf("model = %q", value.Model))
		}
		if value.Reasoning != "" {
			lines = append(lines, fmt.Sprintf("reasoning = %q", value.Reasoning))
		}
		if value.APIKeyEnv != "" {
			lines = append(lines, fmt.Sprintf("api_key_env = %q", value.APIKeyEnv))
		}
		if value.BaseURL != "" {
			lines = append(lines, fmt.Sprintf("base_url = %q", value.BaseURL))
		}
		if value.AnalysisBudget != 0 {
			lines = append(lines, fmt.Sprintf("analysis_budget = %d", value.AnalysisBudget))
		}
	}
	return strings.Join(lines, "\n")
}

func ensureRepoTrust(opts LoadOptions, repoCfg Config) error {
	repoPath, err := filepath.Abs(opts.RepoDir)
	if err != nil {
		return err
	}
	fingerprint, err := Fingerprint(repoCfg)
	if err != nil {
		return err
	}
	trust, err := readTrust(opts.TrustPath)
	if err != nil {
		return err
	}
	if trust.Repos[repoPath] == fingerprint {
		return nil
	}

	fmt.Fprintf(opts.Output, "This repository's config (%s) wants to set analysis options:\n%s\n", repoPath, DescribeProviderSettings(repoCfg))
	if !opts.AcceptRepoTrust && !opts.Yes {
		if !opts.InputIsTTY {
			return fmt.Errorf("these repo settings aren't trusted yet - rerun with --trust-repo-config (or --yes) once you've looked them over")
		}
		fmt.Fprint(opts.Output, "Trust these settings for this repository? [y/N] ")
		reader, ok := opts.Input.(*bufio.Reader)
		if !ok {
			reader = bufio.NewReader(opts.Input)
		}
		answer, readErr := reader.ReadString('\n')
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return fmt.Errorf("read trust confirmation: %w", readErr)
		}
		answer = strings.ToLower(strings.TrimSpace(answer))
		if answer != "y" && answer != "yes" {
			return fmt.Errorf("okay - leaving the repo config untrusted")
		}
	}

	// --yes intentionally covers both confirmation classes in the CLI contract:
	// the cost guard and repository-config trust.
	trust.Repos[repoPath] = fingerprint
	if err := writeTrust(opts.TrustPath, trust); err != nil {
		return fmt.Errorf("store repo config trust: %w", err)
	}
	return nil
}

func readConfig(path string, seed Config) (Config, bool, map[string]bool, error) {
	cfg := seed
	if cfg.Providers == nil {
		cfg.Providers = map[string]ProviderConfig{}
	}
	if path == "" {
		return cfg, false, map[string]bool{}, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, false, map[string]bool{}, nil
	}
	if err != nil {
		return Config{}, false, nil, err
	}
	metadata, err := toml.Decode(string(data), &cfg)
	if err != nil {
		return Config{}, false, nil, err
	}
	if cfg.Providers == nil {
		cfg.Providers = map[string]ProviderConfig{}
	}
	defined := map[string]bool{}
	for _, key := range metadata.Keys() {
		if len(key) > 0 {
			defined[key[0]] = true
			defined[strings.Join(key, ".")] = true
		}
	}
	return cfg, true, defined, nil
}

func applyRepoAllowed(target *Config, value Config, defined map[string]bool, userMedia MediaConfig) {
	viewerFields := []struct {
		key   string
		apply func()
	}{
		{"viewer.collapse_threshold", func() { target.Viewer.CollapseThreshold = value.Viewer.CollapseThreshold }},
		{"viewer.fold_threshold", func() { target.Viewer.FoldThreshold = value.Viewer.FoldThreshold }},
		{"viewer.fold_context", func() { target.Viewer.FoldContext = value.Viewer.FoldContext }},
		{"viewer.long_line_threshold", func() { target.Viewer.LongLineThreshold = value.Viewer.LongLineThreshold }},
		{"viewer.key_symbol_cap", func() { target.Viewer.KeySymbolCap = value.Viewer.KeySymbolCap }},
		{"viewer.word_diff_min_similarity", func() { target.Viewer.WordDiffMinSimilarity = value.Viewer.WordDiffMinSimilarity }},
		{"viewer.theme_light", func() { target.Viewer.ThemeLight = value.Viewer.ThemeLight }},
		{"viewer.theme_dark", func() { target.Viewer.ThemeDark = value.Viewer.ThemeDark }},
		{"media.max_preview_bytes", func() {
			target.Media.MaxPreviewBytes = min(value.Media.MaxPreviewBytes, userMedia.MaxPreviewBytes)
		}},
		{"media.max_total_preview_bytes", func() {
			target.Media.MaxTotalPreviewBytes = min(value.Media.MaxTotalPreviewBytes, userMedia.MaxTotalPreviewBytes)
		}},
		{"media.image_extensions", func() { target.Media.ImageExtensions = append([]string(nil), value.Media.ImageExtensions...) }},
	}
	for _, field := range viewerFields {
		if defined[field.key] {
			field.apply()
		}
	}
}

func readTrust(path string) (trustFile, error) {
	result := trustFile{Repos: map[string]string{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return result, nil
	}
	if err != nil {
		return trustFile{}, err
	}
	if _, err := toml.Decode(string(data), &result); err != nil {
		return trustFile{}, fmt.Errorf("read trust file: %w", err)
	}
	if result.Repos == nil {
		result.Repos = map[string]string{}
	}
	return result, nil
}

func writeTrust(path string, trust trustFile) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	var buffer strings.Builder
	if err := toml.NewEncoder(&buffer).Encode(trust); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".trust-*.toml")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.WriteString(buffer.String()); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempName, path)
}

func cloneProviders(input map[string]ProviderConfig) map[string]ProviderConfig {
	if input == nil {
		return map[string]ProviderConfig{}
	}
	result := make(map[string]ProviderConfig, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}

func cloneBool(value *bool) *bool {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func cloneMedia(value MediaConfig) MediaConfig {
	value.ImageExtensions = append([]string(nil), value.ImageExtensions...)
	return value
}

func mergeViewer(target *ViewerConfig, value ViewerConfig) {
	if value.CollapseThreshold != 0 {
		target.CollapseThreshold = value.CollapseThreshold
	}
	if value.FoldThreshold != 0 {
		target.FoldThreshold = value.FoldThreshold
	}
	if value.FoldContext != 0 {
		target.FoldContext = value.FoldContext
	}
	if value.LongLineThreshold != 0 {
		target.LongLineThreshold = value.LongLineThreshold
	}
	if value.KeySymbolCap != 0 {
		target.KeySymbolCap = value.KeySymbolCap
	}
	if value.WordDiffMinSimilarity != 0 {
		target.WordDiffMinSimilarity = value.WordDiffMinSimilarity
	}
	if value.ThemeLight != "" {
		target.ThemeLight = value.ThemeLight
	}
	if value.ThemeDark != "" {
		target.ThemeDark = value.ThemeDark
	}
}

func mergeMedia(target *MediaConfig, value MediaConfig) {
	if value.MaxPreviewBytes != 0 {
		target.MaxPreviewBytes = value.MaxPreviewBytes
	}
	if value.MaxTotalPreviewBytes != 0 {
		target.MaxTotalPreviewBytes = value.MaxTotalPreviewBytes
	}
	if value.ImageExtensions != nil {
		target.ImageExtensions = append([]string(nil), value.ImageExtensions...)
	}
}

func Validate(cfg Config) error {
	positive := []struct {
		key   string
		value int64
	}{
		{"analysis.summary_batch_max_files", int64(cfg.Analysis.SummaryBatchMaxFiles)},
		{"analysis.stage_concurrency", int64(cfg.Analysis.StageConcurrency)},
		{"analysis.digest_max_files", int64(cfg.Analysis.DigestMaxFiles)},
		{"analysis.digest_max_chars", int64(cfg.Analysis.DigestMaxChars)},
		{"analysis.file_diff_cap", int64(cfg.Analysis.FileDiffCap)},
		{"analysis.guard_call_threshold", int64(cfg.Analysis.GuardCallThreshold)},
		{"analysis.fidelity_budget", int64(cfg.Analysis.FidelityBudget)},
		{"viewer.collapse_threshold", int64(cfg.Viewer.CollapseThreshold)},
		{"viewer.fold_threshold", int64(cfg.Viewer.FoldThreshold)},
		{"viewer.fold_context", int64(cfg.Viewer.FoldContext)},
		{"viewer.long_line_threshold", int64(cfg.Viewer.LongLineThreshold)},
		{"viewer.key_symbol_cap", int64(cfg.Viewer.KeySymbolCap)},
		{"media.max_preview_bytes", cfg.Media.MaxPreviewBytes},
		{"media.max_total_preview_bytes", cfg.Media.MaxTotalPreviewBytes},
		{"github.pr_diff_max_files", int64(cfg.GitHub.PRDiffMaxFiles)},
		{"github.list_limit", int64(cfg.GitHub.ListLimit)},
		{"network.catalog_timeout_seconds", int64(cfg.Network.CatalogTimeoutSeconds)},
		{"network.completion_timeout_seconds", int64(cfg.Network.CompletionTimeoutSeconds)},
		{"network.provider_exec_timeout_seconds", int64(cfg.Network.ProviderExecTimeoutSeconds)},
	}
	for _, item := range positive {
		if item.value <= 0 {
			return fmt.Errorf("%s must be a positive integer", item.key)
		}
	}
	if cfg.Analysis.StagingMaxFiles < 10 || cfg.Analysis.StagingMaxFiles > 500 {
		return fmt.Errorf("analysis.staging_max_files must be between 10 and 500")
	}
	if cfg.Viewer.WordDiffMinSimilarity < 0 || cfg.Viewer.WordDiffMinSimilarity > 1 {
		return fmt.Errorf("viewer.word_diff_min_similarity must be between 0 and 1")
	}
	if cfg.Viewer.FoldContext > cfg.Viewer.FoldThreshold/2 {
		return fmt.Errorf("viewer.fold_context must be at most half of viewer.fold_threshold")
	}
	if cfg.Git.ContextLines < 0 {
		return fmt.Errorf("git.context_lines must be 0 or a positive integer")
	}
	if cfg.Git.FindRenames < 0 || cfg.Git.FindRenames > 100 {
		return fmt.Errorf("git.find_renames must be between 0 and 100")
	}
	if cfg.Cache.MaxEntries < 0 {
		return fmt.Errorf("cache.max_entries must be 0 or a positive integer")
	}
	if strings.TrimSpace(cfg.Viewer.ThemeLight) == "" {
		return fmt.Errorf("viewer.theme_light must not be empty")
	}
	if _, ok := styles.Registry[strings.ToLower(cfg.Viewer.ThemeLight)]; !ok {
		return fmt.Errorf("viewer.theme_light names an unknown Chroma theme %q", cfg.Viewer.ThemeLight)
	}
	if strings.TrimSpace(cfg.Viewer.ThemeDark) == "" {
		return fmt.Errorf("viewer.theme_dark must not be empty")
	}
	if _, ok := styles.Registry[strings.ToLower(cfg.Viewer.ThemeDark)]; !ok {
		return fmt.Errorf("viewer.theme_dark names an unknown Chroma theme %q", cfg.Viewer.ThemeDark)
	}
	if len(cfg.Media.ImageExtensions) == 0 {
		return fmt.Errorf("media.image_extensions must contain at least one extension")
	}
	supportedImages := map[string]bool{
		".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true, ".svg": true,
	}
	for _, extension := range cfg.Media.ImageExtensions {
		if !supportedImages[strings.ToLower(extension)] {
			return fmt.Errorf("media.image_extensions contains unsupported extension %q", extension)
		}
	}
	return nil
}

func UserConfigPath() (string, error) {
	dir, err := userConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "better-git-review", "config.toml"), nil
}

func WriteUser(path string, cfg Config) error {
	if path == "" {
		var err error
		path, err = UserConfigPath()
		if err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	var buffer strings.Builder
	if err := toml.NewEncoder(&buffer).Encode(cfg); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".config-*.toml")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.WriteString(buffer.String()); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempName, path)
}

func userConfigDir() (string, error) {
	return xdg.ConfigHome()
}
