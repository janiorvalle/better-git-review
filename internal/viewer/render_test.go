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
	// Three scripts: the head theme-stamper, the JSON island, and the viewer.
	if strings.Count(html, "<script") != 3 {
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
