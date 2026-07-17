package viewer

import (
	"strings"
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

func TestBuildPageSeparatesMechanicalFromStubbedFiles(t *testing.T) {
	page, err := buildPage(document.Document{
		Files: []document.File{{Path: "generated.go"}, {Path: "failed.go"}},
		Analysis: document.Analysis{
			MechanicalFiles: []int{0},
			StubbedFiles:    []int{1},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !page.Files[0].Mechanical || page.Files[0].Stubbed ||
		page.Files[1].Mechanical || !page.Files[1].Stubbed {
		t.Fatalf("provenance flags = %#v", page.Files)
	}
	if page.MechanicalCount != 1 || len(page.MechanicalFiles) != 1 {
		t.Fatalf("mechanical summary = %#v", page)
	}
}

func TestPlanFullFidelityUsesRenderedSizeThenPath(t *testing.T) {
	files := []document.File{
		{Path: "b.go", Hunks: []document.Hunk{{Lines: []document.HunkLine{{Type: "a", New: 1, Text: "package p"}}}}},
		{Path: "a.go", Hunks: []document.Hunk{{Lines: []document.HunkLine{{Type: "a", New: 1, Text: "package p"}}}}},
		{Path: "large.go", Hunks: []document.Hunk{{Lines: []document.HunkLine{{Type: "a", New: 1, Text: "package " + strings.Repeat("x", 500)}}}}},
	}
	all, err := planFullFidelity(files, nil, 10_000_000)
	if err != nil {
		t.Fatal(err)
	}
	budget := len(all[1])
	selected, err := planFullFidelity(files, nil, budget)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := selected[1]; !ok || len(selected) != 1 {
		t.Fatalf("selected indexes = %#v, want only lexicographically first equal-size file", selected)
	}
}
