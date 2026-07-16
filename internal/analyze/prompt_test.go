package analyze

import (
	"fmt"
	"strings"
	"testing"

	"github.com/janiorvalle/better-git-review/internal/document"
)

func TestBuildPromptPreservesEveryFileHeader(t *testing.T) {
	files := make([]document.File, 400)
	largeText := strings.Repeat("x", 2_000)
	for index := range files {
		files[index] = document.File{
			Path:   fmt.Sprintf("src/file-%03d.go", index),
			Status: "modified",
			Hunks: []document.Hunk{{
				Lines: []document.HunkLine{{Type: "a", Text: largeText}},
			}},
		}
	}
	prompt := BuildPrompt(document.Source{Title: "large"}, files)
	for _, index := range []int{0, 199, 399} {
		header := fmt.Sprintf("===== FILE %d: src/file-%03d.go", index, index)
		if !strings.Contains(prompt, header) {
			t.Fatalf("prompt omitted %q", header)
		}
	}
	if !strings.Contains(prompt, "... [truncated]") {
		t.Fatal("large file bodies were not marked as truncated")
	}
}
