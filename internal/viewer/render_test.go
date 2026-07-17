package viewer

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/janiorvalle/better-git-review/internal/document"
	"github.com/janiorvalle/better-git-review/internal/media"
)

func TestRenderEscapesHostileData(t *testing.T) {
	hostile := `</script><script>alert(1)</script>"></div><img src=x onerror=alert(2)>`
	doc := document.Document{
		SchemaVersion: document.SchemaVersion,
		Source:        document.Source{Title: hostile, Range: hostile},
		Files: []document.File{{
			Path: hostile, Status: "modified", Additions: 1,
			Hunks: []document.Hunk{{
				Header: hostile,
				Blame:  &document.Blame{Author: hostile, Date: "2025-01-01T00:00:00Z"},
				Lines:  []document.HunkLine{{Type: "a", New: 1, Text: hostile}},
			}},
		}},
		Analysis: document.Analysis{
			Title: hostile, Overview: hostile,
			Cohorts: []document.Cohort{
				{
					Title: hostile, Layer: "backend", Intent: hostile, Narrative: hostile,
					Files: []int{0}, FileSummaries: []string{hostile},
					ReviewNotes: []string{hostile}, DependsOn: []int{},
				},
				{
					// Second cohort (with a dependency) forces the native SVG
					// diagram to render hostile cohort titles too.
					Title: hostile, Layer: "config", Intent: hostile, Narrative: hostile,
					Files: []int{}, FileSummaries: []string{},
					ReviewNotes: []string{}, DependsOn: []int{0},
				},
			},
		},
	}
	output, err := Render(doc)
	if err != nil {
		t.Fatal(err)
	}
	html := string(output)
	if index := strings.Index(html, hostile); index >= 0 {
		start := max(0, index-80)
		end := min(len(html), index+len(hostile)+80)
		t.Fatalf("hostile data reached HTML unescaped: %s", html[start:end])
	}
	if strings.Contains(html, `<img src=x onerror`) {
		t.Fatal("hostile image tag reached HTML unescaped")
	}
	// Five scripts: the head theme-stamper, metadata/config/compact-diff
	// islands, and the viewer.
	if strings.Count(html, "<script") != 5 {
		t.Fatalf("unexpected script tags in output: %d", strings.Count(html, "<script"))
	}
	if strings.Contains(html, "mermaid") {
		t.Fatal("mermaid must be fully removed from the viewer")
	}
	island := extractIsland(t, html)
	var decoded document.Document
	if err := json.Unmarshal([]byte(island), &decoded); err != nil {
		t.Fatalf("JSON island is invalid: %v\n%s", err, island)
	}
	if decoded.Files[0].Path != hostile {
		t.Fatalf("JSON island did not preserve hostile text: %q", decoded.Files[0].Path)
	}
}

func TestRenderInjectsViewerThresholdsWithoutJSLiterals(t *testing.T) {
	doc := document.Document{
		SchemaVersion: document.SchemaVersion,
		Source:        document.Source{Title: "Config test", Name: "config-test"},
		Analysis: document.Analysis{
			Title: "Config test", Overview: "Config test", Cohorts: []document.Cohort{},
			StubbedFiles: []int{}, MechanicalFiles: []int{}, FileKeySymbols: [][]string{},
		},
	}
	settings := DefaultSettings()
	settings.CollapseThreshold = 17
	settings.FoldThreshold = 23
	settings.FoldContext = 4
	settings.KeySymbolCap = 9
	output, err := RenderWithSettings(doc, settings)
	if err != nil {
		t.Fatal(err)
	}
	html := string(output)
	for _, expected := range []string{
		`"collapseThreshold":17`, `"foldThreshold":23`,
		`"foldContext":4`, `"keySymbolCap":9`,
	} {
		if !strings.Contains(html, expected) {
			t.Fatalf("artifact missing injected setting %s", expected)
		}
	}
	for _, forbidden := range []string{"> 400)", "> 10)", "start + 3", ".slice(0, 5)"} {
		if strings.Contains(html, forbidden) {
			t.Fatalf("artifact retains duplicated JS literal %q", forbidden)
		}
	}
}

func TestStagedRenderUsesCompactCanonicalDiffPayload(t *testing.T) {
	line := `const value = "</script><script>alert(1)</script>"`
	doc := document.Document{
		SchemaVersion: document.SchemaVersion,
		Meta:          document.Meta{Staged: true},
		Source:        document.Source{Title: "Compact", Range: "main...head"},
		Files: []document.File{{
			Path: "src/app.js", Status: "modified", Additions: 1,
			Hunks: []document.Hunk{{Header: "app", Lines: []document.HunkLine{{
				Type: "a", New: 1, Text: line,
			}}}},
		}},
		Analysis: document.Analysis{
			Title: "Compact", Overview: "Compact",
			Cohorts: []document.Cohort{{
				Title: "App", Layer: "ui", Intent: "Review", Narrative: "Review",
				Files: []int{0}, FileSummaries: []string{"App"},
				ReviewNotes: []string{}, DependsOn: []int{},
			}},
			StubbedFiles: []int{}, MechanicalFiles: []int{},
		},
	}
	output, err := Render(doc)
	if err != nil {
		t.Fatal(err)
	}
	html := string(output)
	if strings.Count(html, line) != 0 {
		t.Fatal("raw staged line escaped its canonical payload")
	}
	if strings.Contains(extractIsland(t, html), `"hunks":[`) {
		t.Fatal("metadata island duplicated staged hunks")
	}
	if strings.Count(html, `Large diff · plain-text rendering`) != 1 ||
		!strings.Contains(html, `id="fidelity-0"`) ||
		!strings.Contains(html, `data-client-files="0"`) ||
		!strings.Contains(html, `function makeClientBody(`) {
		t.Fatal("small staged file did not use planned full-fidelity rendering")
	}
	if !strings.Contains(html, `&lt;/script&gt;`) {
		t.Fatal("full-fidelity rows did not safely escape script delimiters")
	}
}

func TestRenderUsesImgForSVGPreviewOutsideJSONIsland(t *testing.T) {
	doc := document.Document{
		SchemaVersion: document.SchemaVersion,
		Source:        document.Source{Title: "Image", Range: "main...head"},
		Files:         []document.File{{Path: "image.svg", Status: "modified", Binary: true}},
		Analysis: document.Analysis{Title: "Image", Overview: "Image change", Cohorts: []document.Cohort{{
			Title: "Image", Layer: "ui", Intent: "Image", Narrative: "Image",
			Files: []int{0}, FileSummaries: []string{"Image"}, ReviewNotes: []string{}, DependsOn: []int{},
		}}},
	}
	dataURI := "data:image/svg+xml;base64,PHN2ZyBvbmxvYWQ9YWxlcnQoMSk+PC9zdmc+"
	output, err := RenderWithPreviews(doc, map[int]media.Preview{0: {
		Image: true, Old: &media.Asset{DataURI: dataURI, SizeLabel: "1 B"},
		New: &media.Asset{DataURI: dataURI, SizeLabel: "1 B"}, Label: "Binary image",
	}})
	if err != nil {
		t.Fatal(err)
	}
	html := string(output)
	if strings.Count(html, `<img src="data:image/svg&#43;xml;base64,`) != 2 || strings.Contains(html, "<svg onload") {
		t.Fatalf("SVG preview was not isolated in img tags:\n%s", html)
	}
	if strings.Contains(extractIsland(t, html), "data:image") {
		t.Fatal("render-time preview leaked into the JSON island")
	}
}

func TestStagedRenderKeepsImagePreviewInCompactPayload(t *testing.T) {
	doc := document.Document{
		SchemaVersion: document.SchemaVersion,
		Meta:          document.Meta{Staged: true},
		Source:        document.Source{Title: "Image", Range: "main...head"},
		Files:         []document.File{{Path: "image.png", Status: "modified", Binary: true}},
		Analysis: document.Analysis{Title: "Image", Overview: "Image change", Cohorts: []document.Cohort{{
			Title: "Image", Layer: "ui", Intent: "Image", Narrative: "Image",
			Files: []int{0}, FileSummaries: []string{"Image"}, ReviewNotes: []string{}, DependsOn: []int{},
		}}},
	}
	dataURI := "data:image/png;base64,cHJldmlldw=="
	output, err := RenderWithPreviews(doc, map[int]media.Preview{0: {
		Image: true,
		Old:   &media.Asset{DataURI: dataURI, SizeLabel: "7 B", Dimensions: "1x1"},
		Label: "Binary image",
	}})
	if err != nil {
		t.Fatal(err)
	}
	html := string(output)
	if strings.Contains(html, `<img src="data:image/png`) {
		t.Fatal("staged preview was duplicated as server-rendered markup")
	}
	if strings.Count(html, dataURI) != 1 ||
		!strings.Contains(html, `function appendImagePane(`) {
		t.Fatal("staged preview was not preserved in the compact client payload")
	}
	if strings.Contains(extractIsland(t, html), "data:image") {
		t.Fatal("staged preview leaked into the metadata island")
	}
}

func TestRenderShipsLazyUnifiedTemplatesWithoutSplitTables(t *testing.T) {
	doc := document.Document{
		SchemaVersion: document.SchemaVersion,
		Source:        document.Source{Title: "Lazy", Range: "main...head"},
		Files: []document.File{
			{Path: "small.go", Status: "modified", Additions: 1, Hunks: []document.Hunk{{
				Lines: []document.HunkLine{{Type: "a", New: 1, Text: "package small"}},
			}}},
			{Path: "large.go", Status: "modified", Additions: 401, Hunks: []document.Hunk{{
				Lines: []document.HunkLine{{Type: "a", New: 1, Text: "package large"}},
			}}},
		},
		Analysis: document.Analysis{
			Title: "Lazy", Overview: "Lazy bodies",
			Cohorts: []document.Cohort{{
				Title: "Files", Layer: "backend", Intent: "Review", Narrative: "Review",
				Files: []int{0, 1}, FileSummaries: []string{"small", "large"},
				ReviewNotes: []string{}, DependsOn: []int{},
			}},
			StubbedFiles: []int{}, MechanicalFiles: []int{},
		},
	}
	output, err := Render(doc)
	if err != nil {
		t.Fatal(err)
	}
	html := string(output)
	if count := strings.Count(html, `<template class="file-body-template">`); count != 2 {
		t.Fatalf("lazy body templates = %d, want 2", count)
	}
	if strings.Contains(html, `class="diff-table split-table"`) {
		t.Fatal("server-rendered split table survived lazy split conversion")
	}
	for _, marker := range []string{
		`data-split-ready="false"`,
		`function stampFile(file)`,
		`function ensureSplit(file)`,
		`section.querySelectorAll(".file:not(.collapsed)").forEach(stampFile)`,
	} {
		if !strings.Contains(html, marker) {
			t.Fatalf("lazy viewer missing %q", marker)
		}
	}
}

func extractIsland(t *testing.T, html string) string {
	t.Helper()
	startMarker := `<script id="walkthrough-data" type="application/json">`
	start := strings.Index(html, startMarker)
	if start < 0 {
		t.Fatal("JSON island missing")
	}
	start += len(startMarker)
	end := strings.Index(html[start:], "</script>")
	if end < 0 {
		t.Fatal("JSON island closing tag missing")
	}
	return html[start : start+end]
}

func TestRenderShipsKeyboardReviewFlow(t *testing.T) {
	doc := document.Document{
		SchemaVersion: document.SchemaVersion,
		Source:        document.Source{Title: "Keys", Range: "main...head"},
		Files: []document.File{
			{Path: "a.go", Status: "modified", Additions: 1, Hunks: []document.Hunk{{
				Lines: []document.HunkLine{{Type: "a", New: 1, Text: "package a"}},
			}}},
		},
		Analysis: document.Analysis{
			Title: "Keys", Overview: "Keyboard flow",
			Cohorts: []document.Cohort{{
				Title: "Files", Layer: "backend", Intent: "Review", Narrative: "Review",
				Files: []int{0}, FileSummaries: []string{"a"},
				ReviewNotes: []string{}, DependsOn: []int{},
			}},
			StubbedFiles: []int{}, MechanicalFiles: []int{},
		},
	}
	output, err := Render(doc)
	if err != nil {
		t.Fatal(err)
	}
	html := string(output)
	for _, marker := range []string{
		"<kbd>j</kbd><kbd>k</kbd><kbd>v</kbd>",
		"function moveActiveFile(",
		"function jumpToNextUnreviewed(",
		"function toggleViewedCard(",
		"kbd-active",
	} {
		if !strings.Contains(html, marker) {
			t.Fatalf("rendered page is missing keyboard-flow marker %q", marker)
		}
	}
}
