package diff

import (
	"os"
	"path/filepath"
	"strings"
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
	if len(files) != 9 {
		t.Fatalf("got %d files, want 9", len(files))
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
	markers := files[7]
	if markers.Additions != 2 || markers.Deletions != 2 || len(markers.Hunks[0].Lines) != 4 {
		t.Fatalf("hunk content resembling metadata was dropped: %#v", markers)
	}
	if files[8].Path != "docs b/file.txt" {
		t.Fatalf("path containing separator text was misparsed: %#v", files[8])
	}
}

func TestParseRejectsMalformedDiffHeader(t *testing.T) {
	_, err := Parse("diff --git only-one-path\n")
	if err == nil {
		t.Fatal("expected an error")
	}
}

func TestParseCRLFPatch(t *testing.T) {
	patch := strings.ReplaceAll(`diff --git a/a.txt b/a.txt
index 1111111..2222222 100644
--- a/a.txt
+++ b/a.txt
@@ -1 +1 @@
-old
+new
`, "\n", "\r\n")
	files, err := Parse(patch)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Path != "a.txt" || files[0].Hunks[0].Lines[1].Text != "new" {
		t.Fatalf("CRLF patch was corrupted: %#v", files)
	}
}

func TestParseStopsAtFormatPatchTrailer(t *testing.T) {
	patch := strings.Join([]string{
		"diff --git a/a.txt b/a.txt",
		"index 1111111..2222222 100644",
		"--- a/a.txt",
		"+++ b/a.txt",
		"@@ -1 +1 @@",
		"-old",
		"+new",
		"-- ",
		"2.50.0",
		"",
	}, "\n")
	files, err := Parse(patch)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Additions != 1 || files[0].Deletions != 1 ||
		len(files[0].Hunks[0].Lines) != 2 {
		t.Fatalf("format-patch trailer was parsed as hunk content: %#v", files)
	}
}

func TestParseMetadataPathPreservesWhitespace(t *testing.T) {
	path := " report.txt "
	if got := parseMetadataPath(path); got != path {
		t.Fatalf("got %q, want %q", got, path)
	}
}
