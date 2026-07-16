package diff

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseFeatureFixtures(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "features.patch"))
	if err != nil {
		t.Fatal(err)
	}
	files, err := Parse(string(data))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 7 {
		t.Fatalf("got %d files, want 7", len(files))
	}

	added := files[0]
	if added.Status != "added" || added.Path != "new.txt" || added.Additions != 2 || added.Deletions != 0 {
		t.Fatalf("unexpected added file: %#v", added)
	}
	if got := added.Hunks[0].Lines[0]; got.Type != "a" || got.Old != 0 || got.New != 1 || got.Text != "first" {
		t.Fatalf("unexpected added line: %#v", got)
	}
	if len(added.Hunks[0].Lines) != 2 {
		t.Fatalf("trailing newline produced a synthetic context line: %#v", added.Hunks[0].Lines)
	}

	deleted := files[1]
	if deleted.Status != "deleted" || deleted.Path != "old.txt" || deleted.Deletions != 2 {
		t.Fatalf("unexpected deleted file: %#v", deleted)
	}
	if got := deleted.Hunks[0].Lines[0]; got.Type != "d" || got.Old != 1 || got.New != 0 {
		t.Fatalf("unexpected deleted line: %#v", got)
	}

	renamed := files[2]
	if renamed.Status != "renamed" || renamed.OldPath != "before.go" || renamed.NewPath != "after.go" || renamed.Path != "after.go" {
		t.Fatalf("unexpected renamed file: %#v", renamed)
	}
	if renamed.Hunks[0].Header != "func renamed() {" {
		t.Fatalf("unexpected hunk header %q", renamed.Hunks[0].Header)
	}
	if got := renamed.Hunks[0].Lines[2]; got.Type != "c" || got.Old != 11 || got.New != 11 {
		t.Fatalf("unexpected context line: %#v", got)
	}

	if !files[3].Binary || files[3].Status != "added" {
		t.Fatalf("unexpected binary file: %#v", files[3])
	}
	if files[4].Path != "docs/café guide.md" || files[4].Hunks[0].Header != "heading context" {
		t.Fatalf("quoted path or header not parsed: %#v", files[4])
	}
	modeOnly := files[5]
	if modeOnly.Status != "modified" || len(modeOnly.Hunks) != 0 {
		t.Fatalf("unexpected mode-only file: %#v", modeOnly)
	}
	if files[6].Path != "file with spaces.txt" {
		t.Fatalf("unquoted spaced path not parsed: %#v", files[6])
	}
}

func TestParseRejectsMalformedDiffHeader(t *testing.T) {
	_, err := Parse("diff --git only-one-path\n")
	if err == nil {
		t.Fatal("expected an error")
	}
}
