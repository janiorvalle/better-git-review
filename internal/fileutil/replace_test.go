package fileutil

import (
	"errors"
	"testing"
)

func TestReplaceRetriesAfterRemovingWindowsTarget(t *testing.T) {
	var (
		renameCalls int
		removed     bool
	)
	err := replace(
		"windows",
		"temp",
		"target",
		func(string, string) error {
			renameCalls++
			if renameCalls == 1 {
				return errors.New("target exists")
			}
			return nil
		},
		func(path string) error {
			removed = path == "target"
			return nil
		},
	)
	if err != nil || renameCalls != 2 || !removed {
		t.Fatalf("err=%v renameCalls=%d removed=%v", err, renameCalls, removed)
	}
}

func TestReplaceDoesNotRemoveUnixTargetAfterRenameFailure(t *testing.T) {
	want := errors.New("rename failed")
	removed := false
	err := replace(
		"linux",
		"temp",
		"target",
		func(string, string) error { return want },
		func(string) error {
			removed = true
			return nil
		},
	)
	if !errors.Is(err, want) || removed {
		t.Fatalf("err=%v removed=%v", err, removed)
	}
}
