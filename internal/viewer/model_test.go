package viewer

import (
	"testing"

	"github.com/janiorvalle/better-git-review/internal/document"
)

func TestBuildPageCollapsesLargeFiles(t *testing.T) {
	page, err := buildPage(document.Document{
		Files: []document.File{
			{Path: "small.go", Additions: 200, Deletions: 200},
			{Path: "large.go", Additions: 201, Deletions: 200},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if page.Files[0].Collapsed {
		t.Fatal("file with 400 changed lines should start expanded")
	}
	if !page.Files[1].Collapsed {
		t.Fatal("file with more than 400 changed lines should start collapsed")
	}
}

func TestBuildPageFlagsStubbedFiles(t *testing.T) {
	page, err := buildPage(document.Document{
		Files: []document.File{{Path: "first.go"}, {Path: "second.go"}},
		Analysis: document.Analysis{
			StubbedFiles: []int{1},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if page.Files[0].Stubbed || !page.Files[1].Stubbed {
		t.Fatalf("stub flags = %#v", page.Files)
	}
}
