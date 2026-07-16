package viewer

import (
	"regexp"
	"strings"
	"testing"

	"github.com/janiorvalle/better-git-review/internal/document"
)

func TestChangedSpans(t *testing.T) {
	tests := []struct {
		name    string
		oldText string
		newText string
		oldSpan Span
		newSpan Span
		changed bool
	}{
		{"empty", "", "", Span{}, Span{}, false},
		{"identical", "same", "same", Span{}, Span{}, false},
		{"fully different", "old", "new", Span{0, 3}, Span{0, 3}, true},
		{"common edges", "hello old world", "hello new world", Span{6, 9}, Span{6, 9}, true},
		{"utf8", "café", "caff", Span{3, 4}, Span{3, 4}, true},
		{"whitespace", "a b", "a  b", Span{2, 2}, Span{2, 3}, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			oldSpan, newSpan, changed := ChangedSpans(test.oldText, test.newText)
			if oldSpan != test.oldSpan || newSpan != test.newSpan || changed != test.changed {
				t.Fatalf("got %#v %#v %v", oldSpan, newSpan, changed)
			}
		})
	}
}

func TestPairingMarksOverlapForUnequalBlocks(t *testing.T) {
	lines := []document.HunkLine{
		{Type: "d", Text: "old one"}, {Type: "d", Text: "old two"},
		{Type: "a", Text: "new one"},
	}
	pairs := pairChangedLines(lines)
	if pairs[0].oldSpan == nil || pairs[2].newSpan == nil {
		t.Fatalf("first overlap was not paired: %#v", pairs)
	}
	if _, paired := pairs[1]; paired {
		t.Fatalf("extra deletion should not be paired: %#v", pairs[1])
	}
}

func TestSplitContextUsesEachSideLineNumber(t *testing.T) {
	rows := buildSplitLines(
		[]document.HunkLine{{Type: "c", Old: 8, New: 10, Text: "context"}},
		nil,
		newHighlighter("main.go"),
	)
	if len(rows) != 1 || rows[0].Old.Number != 8 || rows[0].New.Number != 10 {
		t.Fatalf("split context line numbers = %#v", rows)
	}
}

func TestFoldingBoundary(t *testing.T) {
	build := func(count int) []UnifiedRow {
		rows := make([]UnifiedRow, count)
		for index := range rows {
			rows[index] = UnifiedRow{Kind: "line", Class: "c"}
		}
		applyFolds(
			rows,
			"fold",
			func(row UnifiedRow) bool { return row.Kind == "line" && row.Class == "c" },
			func(row *UnifiedRow, foldID string, foldCount int) {
				row.Hidden = true
				row.FoldID = foldID
				row.FoldCount = foldCount
			},
		)
		return rows
	}
	atThreshold := build(FoldThreshold)
	for _, row := range atThreshold {
		if row.Hidden {
			t.Fatal("exact threshold should not fold")
		}
	}
	overThreshold := build(FoldThreshold + 1)
	hidden := 0
	for _, row := range overThreshold {
		if row.Hidden {
			hidden++
		}
	}
	if hidden != FoldThreshold+1-6 || overThreshold[3].FoldCount != hidden {
		t.Fatalf("unexpected fold: hidden=%d rows=%#v", hidden, overThreshold)
	}
}

func TestFoldingStaysWithinHunks(t *testing.T) {
	contextLines := func(offset int) []document.HunkLine {
		lines := make([]document.HunkLine, FoldThreshold+1)
		for index := range lines {
			lines[index] = document.HunkLine{
				Type: "c",
				Old:  offset + index,
				New:  offset + index,
				Text: "context",
			}
		}
		return lines
	}
	file := document.File{
		Path: "main.go",
		Hunks: []document.Hunk{
			{Header: "first", Lines: contextLines(1)},
			{Header: "second", Lines: contextLines(100)},
		},
	}
	unified, split := BuildRows(file, 3)
	unifiedIDs := map[string]bool{}
	for _, row := range unified {
		if row.FoldCount > 0 {
			unifiedIDs[row.FoldID] = true
		}
	}
	if len(unifiedIDs) != 2 || !unifiedIDs["u-3-0"] || !unifiedIDs["u-3-1"] {
		t.Fatalf("unified folds crossed hunk boundaries: %#v", unifiedIDs)
	}
	ids := map[string]bool{}
	for _, row := range split {
		if row.FoldCount > 0 {
			ids[row.FoldID] = true
		}
	}
	if len(ids) != 2 || !ids["s-3-0"] || !ids["s-3-1"] {
		t.Fatalf("split folds crossed hunk boundaries: %#v", ids)
	}
}

func TestHighlightKnownAndUnknownExtensions(t *testing.T) {
	known := newHighlighter("main.go").highlight("func main() {}", nil)
	if !strings.Contains(string(known), `class="kd"`) {
		t.Fatalf("Go was not syntax highlighted: %s", known)
	}
	unknown := newHighlighter("file.unknownextension").highlight("plain < text", nil)
	if string(unknown) != "plain &lt; text" {
		t.Fatalf("unknown extension should be escaped plain text: %s", unknown)
	}
}

func TestChromaThemeUsesCompleteVariablePaletteWithoutBackgrounds(t *testing.T) {
	theme, err := ChromaThemeCSS("github", "github-dark")
	if err != nil {
		t.Fatal(err)
	}
	combined := string(theme.TokenCSS) + string(theme.LightVariables) + string(theme.DarkVariables)
	if strings.Contains(combined, "background") {
		t.Fatalf("generated Chroma theme still sets backgrounds:\n%s", combined)
	}
	if strings.Contains(combined, ": ;") {
		t.Fatalf("generated Chroma theme contains an empty variable:\n%s", combined)
	}
	variablePattern := regexp.MustCompile(`var\((--chroma-[^)]+)\)`)
	for _, match := range variablePattern.FindAllStringSubmatch(string(theme.TokenCSS), -1) {
		variable := match[1] + ":"
		if !strings.Contains(string(theme.LightVariables), variable) {
			t.Errorf("light palette does not define %s", match[1])
		}
		if !strings.Contains(string(theme.DarkVariables), variable) {
			t.Errorf("dark palette does not define %s", match[1])
		}
	}
	if !strings.Contains(string(theme.DarkVariables), "#e6edf3") {
		t.Fatal("dark palette does not carry the GitHub-dark foreground fallback")
	}
}
