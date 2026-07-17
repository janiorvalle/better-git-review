package cache

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/janiorvalle/better-git-review/internal/document"
)

func TestKeyChangesForEveryInput(t *testing.T) {
	base := Key([]byte("diff"), "mock", "one", "low", 1)
	values := []string{
		Key([]byte("different"), "mock", "one", "low", 1),
		Key([]byte("diff"), "claude-cli", "one", "low", 1),
		Key([]byte("diff"), "mock", "two", "low", 1),
		Key([]byte("diff"), "mock", "one", "high", 1),
		Key([]byte("diff"), "mock", "one", "low", 2),
	}
	for _, value := range values {
		if value == base {
			t.Fatal("key input did not affect key")
		}
	}
}

func TestStorePrunesOldestModificationTime(t *testing.T) {
	dir := t.TempDir()
	store := Cache{Dir: dir, MaxEntries: 2}
	keys := []string{
		Key([]byte("old"), "mock", "", "", 1),
		Key([]byte("middle"), "mock", "", "", 1),
		Key([]byte("new"), "mock", "", "", 1),
	}
	for index, key := range keys {
		if err := store.Store(key, document.Document{SchemaVersion: document.SchemaVersion}); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, key+".json")
		when := time.Unix(int64(index+1), 0)
		if err := os.Chtimes(path, when, when); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, keys[0]+".json")); !os.IsNotExist(err) {
		t.Fatalf("oldest entry was not pruned: %v", err)
	}
	for _, key := range keys[1:] {
		if _, err := os.Stat(filepath.Join(dir, key+".json")); err != nil {
			t.Fatalf("%s entry missing: %v", key, err)
		}
	}
}

func TestPruneIgnoresInProgressFiles(t *testing.T) {
	dir := t.TempDir()
	temp := filepath.Join(dir, ".cache-in-progress.json")
	if err := os.WriteFile(temp, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := Cache{Dir: dir, MaxEntries: 1}
	if err := store.Store(Key([]byte("done"), "mock", "", "", 1), document.Document{SchemaVersion: document.SchemaVersion}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(temp); err != nil {
		t.Fatalf("in-progress file was pruned: %v", err)
	}
}

func TestCacheRoundTripAndCorruption(t *testing.T) {
	store := Cache{
		Dir: t.TempDir(),
		Validate: func(value document.Document) error {
			if value.Analysis.Title == "" || value.Analysis.Overview == "" {
				return fmt.Errorf("incomplete analysis")
			}
			return nil
		},
	}
	key := Key([]byte("diff"), "mock", "deterministic", "", 1)
	value := document.Document{
		SchemaVersion: document.SchemaVersion,
		Files:         []document.File{{Path: "a.go"}},
		Analysis: document.Analysis{
			Title: "Change", Overview: "Overview", StubbedFiles: []int{}, MechanicalFiles: []int{},
			Cohorts: []document.Cohort{{
				Title: "Backend", Layer: "backend", Intent: "Change backend", Narrative: "Review the backend change.",
				Files:         []int{0},
				FileSummaries: []string{"changed"}, ReviewNotes: []string{}, DependsOn: []int{},
			}},
		},
		Meta: document.Meta{Cached: false},
	}
	if err := store.Store(key, value); err != nil {
		t.Fatal(err)
	}
	got, ok := store.Load(key)
	if !ok || !got.Meta.Cached {
		t.Fatalf("cache miss or cached flag false: %#v, %v", got, ok)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var missingStaged map[string]any
	if err := json.Unmarshal(encoded, &missingStaged); err != nil {
		t.Fatal(err)
	}
	delete(missingStaged["meta"].(map[string]any), "staged")
	encoded, err = json.Marshal(missingStaged)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store.Dir, key+".json"), encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Load(key); ok {
		t.Fatal("cache entry missing meta.staged should be a miss")
	}
	if err := os.WriteFile(filepath.Join(store.Dir, key+".json"), []byte("{bad"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Load(key); ok {
		t.Fatal("corrupt cache entry should be a miss")
	}

	value.Analysis.Title = ""
	value.Analysis.Overview = ""
	encoded, err = json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store.Dir, key+".json"), encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Load(key); ok {
		t.Fatal("schema-invalid cache entry should be a miss")
	}
}
