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
	"github.com/janiorvalle/better-git-review/internal/xdg"
)

type Config struct {
	Provider  string                    `toml:"provider"`
	AutoOpen  *bool                     `toml:"auto_open"`
	Providers map[string]ProviderConfig `toml:"providers"`
}

type ProviderConfig struct {
	Model     string `toml:"model" json:"model,omitempty"`
	Reasoning string `toml:"reasoning" json:"reasoning,omitempty"`
	APIKeyEnv string `toml:"api_key_env" json:"api_key_env,omitempty"`
	BaseURL   string `toml:"base_url" json:"base_url,omitempty"`
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

	userCfg, userFound, err := readConfig(opts.UserConfigPath)
	if err != nil {
		return Loaded{}, fmt.Errorf("read user config: %w", err)
	}
	repoCfg, repoFound, err := readConfig(opts.RepoConfigPath)
	if err != nil {
		return Loaded{}, fmt.Errorf("read repo config: %w", err)
	}
	if repoFound && HasProviderSettings(repoCfg) {
		if err := ensureRepoTrust(opts, repoCfg); err != nil {
			return Loaded{}, err
		}
	}

	merged := Merge(userCfg, repoCfg)
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
		Provider:  base.Provider,
		AutoOpen:  cloneBool(base.AutoOpen),
		Providers: cloneProviders(base.Providers),
	}
	if override.Provider != "" {
		result.Provider = override.Provider
	}
	if override.AutoOpen != nil {
		result.AutoOpen = cloneBool(override.AutoOpen)
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
		result.Providers[name] = current
	}
	return result
}

func HasProviderSettings(cfg Config) bool {
	return cfg.Provider != "" || len(cfg.Providers) > 0
}

func Fingerprint(cfg Config) (string, error) {
	type providerEntry struct {
		Name   string         `json:"name"`
		Config ProviderConfig `json:"config"`
	}
	canonical := struct {
		Provider  string          `json:"provider"`
		Providers []providerEntry `json:"providers"`
	}{Provider: cfg.Provider}
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

func DescribeProviderSettings(cfg Config) string {
	var lines []string
	if cfg.Provider != "" {
		lines = append(lines, fmt.Sprintf("provider = %q", cfg.Provider))
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

	fmt.Fprintf(opts.Output, "This repository's config (%s) wants to set your provider:\n%s\n", repoPath, DescribeProviderSettings(repoCfg))
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

func readConfig(path string) (Config, bool, error) {
	cfg := Config{Providers: map[string]ProviderConfig{}}
	if path == "" {
		return cfg, false, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, false, nil
	}
	if err != nil {
		return Config{}, false, err
	}
	if _, err := toml.Decode(string(data), &cfg); err != nil {
		return Config{}, false, err
	}
	if cfg.Providers == nil {
		cfg.Providers = map[string]ProviderConfig{}
	}
	return cfg, true, nil
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
