package config

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMergePrecedence(t *testing.T) {
	user := Config{
		Provider: "claude-cli",
		Providers: map[string]ProviderConfig{
			"claude-cli": {Model: "haiku"},
			"openrouter": {Model: "user-model", APIKeyEnv: "USER_KEY"},
		},
	}
	repo := Config{
		Provider: "openrouter",
		Providers: map[string]ProviderConfig{
			"openrouter": {Model: "repo-model", BaseURL: "https://repo.example"},
		},
	}
	got := Merge(user, repo)
	if got.Provider != "openrouter" {
		t.Fatalf("provider = %q", got.Provider)
	}
	openrouter := got.Providers["openrouter"]
	if openrouter.Model != "repo-model" || openrouter.APIKeyEnv != "USER_KEY" ||
		openrouter.BaseURL != "https://repo.example" {
		t.Fatalf("unexpected merged provider: %#v", openrouter)
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
