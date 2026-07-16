package viewer

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/janiorvalle/better-git-review/internal/document"
)

func TestRenderEscapesHostileData(t *testing.T) {
	hostile := `</script><script>alert(1)</script>"></div><img src=x onerror=alert(2)>`
	diagram := "graph LR\nA --> B"
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
			Title: hostile, Overview: hostile, Mermaid: &diagram,
			Cohorts: []document.Cohort{{
				Title: hostile, Layer: "backend", Intent: hostile, Narrative: hostile,
				Files: []int{0}, FileSummaries: []string{hostile},
				ReviewNotes: []string{hostile}, DependsOn: []int{},
			}},
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
	if strings.Count(html, "<script") != 3 {
		t.Fatalf("unexpected script tags in output: %d", strings.Count(html, "<script"))
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
