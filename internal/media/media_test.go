package media

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/janiorvalle/better-git-review/internal/document"
)

func TestImagePreviewUsesBaseAndHeadRefs(t *testing.T) {
	pngData := testPNG(t, 2, 3)
	runner := &assetRunner{content: pngData}
	previews := Enrich(context.Background(), []document.File{{
		Path: "image.png", OldPath: "old.png", NewPath: "image.png", Status: "modified", Binary: true,
	}}, Source{RepoDir: "/repo", BaseRef: "base", HeadRef: "head"}, runner)
	preview := previews[0]
	if preview.Old == nil || preview.New == nil || preview.Old.Dimensions != "2 x 3" || preview.New.Dimensions != "2 x 3" {
		t.Fatalf("preview = %#v", preview)
	}
	joined := strings.Join(runner.calls, "\n")
	if !strings.Contains(joined, "merge:old.png") || !strings.Contains(joined, "head:image.png") {
		t.Fatalf("blob refs were not selected correctly:\n%s", joined)
	}
}

func TestDirtyPreviewRejectsSymlink(t *testing.T) {
	repo := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.png")
	if err := os.WriteFile(outside, testPNG(t, 1, 1), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(repo, "image.png")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	previews := Enrich(context.Background(), []document.File{{
		Path: "image.png", OldPath: "image.png", NewPath: "image.png", Status: "modified", Binary: true,
	}}, Source{RepoDir: repo, BaseRef: "HEAD", HeadRef: "WORKTREE", Dirty: true}, &assetRunner{content: testPNG(t, 1, 1)})
	if previews[0].New != nil {
		t.Fatalf("symlink content was loaded: %#v", previews[0].New)
	}
}

func TestPreviewCapAvoidsBlobRead(t *testing.T) {
	runner := &assetRunner{size: MaxPreviewBytes + 1}
	previews := Enrich(context.Background(), []document.File{{
		Path: "image.png", OldPath: "image.png", NewPath: "image.png", Status: "modified", Binary: true,
	}}, Source{RepoDir: "/repo", BaseRef: "base", HeadRef: "head"}, runner)
	if previews[0].Old != nil || previews[0].New != nil || !strings.Contains(previews[0].Label, "1.6 MB") {
		t.Fatalf("over-cap preview = %#v", previews[0])
	}
	for _, call := range runner.calls {
		if strings.Contains(call, "cat-file -p") {
			t.Fatalf("over-cap blob was read: %s", call)
		}
	}
}

func TestPreviewAggregateBudgetBoundsBlobReads(t *testing.T) {
	runner := &assetRunner{content: make([]byte, 1<<20)}
	files := make([]document.File, 7)
	for index := range files {
		path := fmt.Sprintf("image-%d.png", index)
		files[index] = document.File{Path: path, OldPath: path, NewPath: path, Status: "modified", Binary: true}
	}
	Enrich(context.Background(), files, Source{RepoDir: "/repo", BaseRef: "base", HeadRef: "head"}, runner)
	contentReads := 0
	for _, call := range runner.calls {
		if strings.Contains(call, "cat-file -p") {
			contentReads++
		}
	}
	if contentReads != MaxTotalPreviewBytes/(1<<20) {
		t.Fatalf("content reads = %d, calls = %#v", contentReads, runner.calls)
	}
}

func TestPatchImageGetsHonestLabel(t *testing.T) {
	previews := Enrich(context.Background(), []document.File{{Path: "image.svg", Binary: true}}, Source{}, nil)
	if got := previews[0].Label; got != "Binary image \u00b7 content not available from patch input." {
		t.Fatalf("label = %q", got)
	}
}

func TestModifiedImageDoesNotPresentUnavailableBlobAsMissingSide(t *testing.T) {
	runner := &assetRunner{content: testPNG(t, 2, 2), failSpec: "merge:old.png"}
	previews := Enrich(context.Background(), []document.File{{
		Path: "image.png", OldPath: "old.png", NewPath: "image.png", Status: "modified", Binary: true,
	}}, Source{RepoDir: "/repo", BaseRef: "base", HeadRef: "head"}, runner)
	preview := previews[0]
	if preview.Old != nil || preview.New != nil || !strings.Contains(preview.Label, "unavailable") || strings.Contains(preview.Label, "added") {
		t.Fatalf("preview = %#v", preview)
	}
}

type assetRunner struct {
	content  []byte
	size     int
	calls    []string
	failSpec string
}

func (r *assetRunner) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, strings.Join(args, " "))
	if len(args) >= 1 && args[0] == "merge-base" {
		return []byte("merge\n"), nil
	}
	if len(args) >= 2 && args[0] == "cat-file" && args[1] == "-s" {
		if len(args) > 2 && args[2] == r.failSpec {
			return nil, fmt.Errorf("missing blob")
		}
		size := r.size
		if size == 0 {
			size = len(r.content)
		}
		return []byte(strconv.Itoa(size)), nil
	}
	if len(args) >= 2 && args[0] == "cat-file" && args[1] == "-p" {
		return r.content, nil
	}
	return nil, fmt.Errorf("unexpected call")
}

func testPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	value := image.NewRGBA(image.Rect(0, 0, width, height))
	value.Set(0, 0, color.RGBA{R: 255, A: 255})
	var output bytes.Buffer
	if err := png.Encode(&output, value); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
