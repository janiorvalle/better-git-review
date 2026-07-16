package cache

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/janiorvalle/better-git-review/internal/document"
)

func TestKeyChangesForEveryInput(t *testing.T) {
	base := Key([]byte("diff"), "mock", "one", 1)
	values := []string{
		Key([]byte("different"), "mock", "one", 1),
		Key([]byte("diff"), "claude-cli", "one", 1),
		Key([]byte("diff"), "mock", "two", 1),
		Key([]byte("diff"), "mock", "one", 2),
	}
	for _, value := range values {
		if value == base {
			t.Fatal("key input did not affect key")
		}
	}
}

func TestCacheRoundTripAndCorruption(t *testing.T) {
	store := Cache{Dir: t.TempDir()}
	key := Key([]byte("diff"), "mock", "deterministic", 1)
	value := document.Document{
		SchemaVersion: document.SchemaVersion,
		Files:         []document.File{{Path: "a.go"}},
		Analysis: document.Analysis{Cohorts: []document.Cohort{{
			Title: "Backend", Layer: "backend", Files: []int{0},
			FileSummaries: []string{"changed"}, ReviewNotes: []string{}, DependsOn: []int{},
		}}},
		Meta: document.Meta{Cached: false},
	}
	if err := store.Store(key, value); err != nil {
		t.Fatal(err)
	}
	got, ok := store.Load(key)
	if !ok || !got.Meta.Cached {
		t.Fatalf("cache miss or cached flag false: %#v, %v", got, ok)
	}
	if err := os.WriteFile(filepath.Join(store.Dir, key+".json"), []byte("{bad"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Load(key); ok {
		t.Fatal("corrupt cache entry should be a miss")
	}
}
