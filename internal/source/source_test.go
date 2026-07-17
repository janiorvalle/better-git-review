package source

import (
	"context"
	"errors"
	"slices"
	"testing"
)

func TestRegistrySelectsFirstDetectedSource(t *testing.T) {
	registry := NewRegistry(
		fakeSource{name: "first", available: false},
		fakeSource{name: "second", available: true, result: Result{Diff: []byte("diff")}},
		fakeSource{name: "third", available: true},
	)
	result, err := registry.Collect(context.Background(), Options{RepoDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if string(result.Diff) != "diff" {
		t.Fatalf("unexpected result: %#v", result)
	}
	if names := registry.Names(); !slices.Equal(names, []string{"first", "second", "third"}) {
		t.Fatalf("names = %#v", names)
	}
}

func TestRegistryAnnotatesCollectionErrors(t *testing.T) {
	registry := NewRegistry(fakeSource{name: "github", available: true, err: errors.New("boom")})
	if _, err := registry.Collect(context.Background(), Options{RepoDir: t.TempDir()}); err == nil ||
		err.Error() != "github source: boom" {
		t.Fatalf("unexpected error: %v", err)
	}
}

type fakeSource struct {
	name      string
	available bool
	result    Result
	err       error
}

func (f fakeSource) Name() string {
	return f.name
}

func (f fakeSource) Detect(Options) (bool, string) {
	return f.available, "test"
}

func (f fakeSource) Collect(context.Context, Options) (Result, error) {
	return f.result, f.err
}
