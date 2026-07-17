package viewer

import (
	"bytes"
	"fmt"
	"html/template"
	"regexp"
	"sort"
	"strings"

	"github.com/alecthomas/chroma/v2"
	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/janiorvalle/better-git-review/internal/document"
)

const FoldThreshold = 10

type Span struct {
	Start int
	End   int
}

type LineCell struct {
	Number      int
	Code        template.HTML
	Class       string
	Placeholder bool
}

type UnifiedRow struct {
	Kind      string
	Class     string
	Old       int
	New       int
	Prefix    string
	Code      template.HTML
	Header    string
	Blame     *document.Blame
	FoldID    string
	FoldCount int
	Hidden    bool
}

type SplitRow struct {
	Kind      string
	Old       LineCell
	New       LineCell
	Header    string
	Blame     *document.Blame
	FoldID    string
	FoldCount int
	Hidden    bool
}

type linePair struct {
	oldSpan *Span
	newSpan *Span
}

func ChangedSpans(oldText, newText string) (Span, Span, bool) {
	oldRunes := []rune(oldText)
	newRunes := []rune(newText)
	prefix := 0
	for prefix < len(oldRunes) && prefix < len(newRunes) && oldRunes[prefix] == newRunes[prefix] {
		prefix++
	}
	if prefix == len(oldRunes) && prefix == len(newRunes) {
		return Span{}, Span{}, false
	}
	suffix := 0
	for suffix < len(oldRunes)-prefix && suffix < len(newRunes)-prefix &&
		oldRunes[len(oldRunes)-1-suffix] == newRunes[len(newRunes)-1-suffix] {
		suffix++
	}
	return Span{Start: prefix, End: len(oldRunes) - suffix},
		Span{Start: prefix, End: len(newRunes) - suffix}, true
}

func BuildRows(file document.File, fileIndex int) ([]UnifiedRow, []SplitRow) {
	highlighter := newHighlighter(file.Path)
	var unified []UnifiedRow
	var split []SplitRow
	for hunkIndex, hunk := range file.Hunks {
		if hunk.Blame != nil {
			unified = append(unified, UnifiedRow{Kind: "blame", Blame: hunk.Blame})
			split = append(split, SplitRow{Kind: "blame", Blame: hunk.Blame})
		}
		header := hunkLabel(hunk)
		unified = append(unified, UnifiedRow{Kind: "hunk", Header: header})
		split = append(split, SplitRow{Kind: "hunk", Header: header})

		pairs := pairChangedLines(hunk.Lines)
		startUnified := len(unified)
		for lineIndex, line := range hunk.Lines {
			var span *Span
			if pair, ok := pairs[lineIndex]; ok {
				if line.Type == "d" {
					span = pair.oldSpan
				} else if line.Type == "a" {
					span = pair.newSpan
				}
			}
			unified = append(unified, UnifiedRow{
				Kind:   "line",
				Class:  line.Type,
				Old:    line.Old,
				New:    line.New,
				Prefix: linePrefix(line.Type),
				Code:   highlighter.highlight(line.Text, span),
			})
		}
		applyFolds(
			unified[startUnified:],
			fmt.Sprintf("u-%d-%d", fileIndex, hunkIndex),
			func(row UnifiedRow) bool { return row.Kind == "line" && row.Class == "c" },
			func(row *UnifiedRow, foldID string, foldCount int) {
				row.Hidden = true
				row.FoldID = foldID
				row.FoldCount = foldCount
			},
		)

		startSplit := len(split)
		split = append(split, buildSplitLines(hunk.Lines, pairs, highlighter)...)
		applyFolds(
			split[startSplit:],
			fmt.Sprintf("s-%d-%d", fileIndex, hunkIndex),
			isSplitContext,
			func(row *SplitRow, foldID string, foldCount int) {
				row.Hidden = true
				row.FoldID = foldID
				row.FoldCount = foldCount
			},
		)
	}
	return unified, split
}

func pairChangedLines(lines []document.HunkLine) map[int]linePair {
	result := map[int]linePair{}
	for index := 0; index < len(lines); {
		if lines[index].Type != "d" {
			index++
			continue
		}
		deleteStart := index
		for index < len(lines) && lines[index].Type == "d" {
			index++
		}
		deleteEnd := index
		addStart := index
		for index < len(lines) && lines[index].Type == "a" {
			index++
		}
		if addStart == index {
			continue
		}
		count := min(deleteEnd-deleteStart, index-addStart)
		for offset := 0; offset < count; offset++ {
			oldSpan, newSpan, changed := ChangedSpans(lines[deleteStart+offset].Text, lines[addStart+offset].Text)
			if !changed {
				continue
			}
			oldCopy, newCopy := oldSpan, newSpan
			result[deleteStart+offset] = linePair{oldSpan: &oldCopy}
			result[addStart+offset] = linePair{newSpan: &newCopy}
		}
	}
	return result
}

func buildSplitLines(lines []document.HunkLine, pairs map[int]linePair, highlighter *syntaxHighlighter) []SplitRow {
	var rows []SplitRow
	for index := 0; index < len(lines); {
		if lines[index].Type == "d" {
			deleteStart := index
			for index < len(lines) && lines[index].Type == "d" {
				index++
			}
			deleteEnd := index
			addStart := index
			for index < len(lines) && lines[index].Type == "a" {
				index++
			}
			addEnd := index
			count := max(deleteEnd-deleteStart, addEnd-addStart)
			for offset := 0; offset < count; offset++ {
				row := SplitRow{Kind: "line"}
				if deleteStart+offset < deleteEnd {
					lineIndex := deleteStart + offset
					row.Old = cellForLine(lines[lineIndex], highlighter, pairs[lineIndex].oldSpan)
				} else {
					row.Old = LineCell{Placeholder: true}
				}
				if addStart+offset < addEnd {
					lineIndex := addStart + offset
					row.New = cellForLine(lines[lineIndex], highlighter, pairs[lineIndex].newSpan)
				} else {
					row.New = LineCell{Placeholder: true}
				}
				rows = append(rows, row)
			}
			continue
		}
		line := lines[index]
		if line.Type == "a" {
			rows = append(rows, SplitRow{
				Kind: "line",
				Old:  LineCell{Placeholder: true},
				New:  cellForLine(line, highlighter, nil),
			})
		} else {
			oldCell := cellForLine(line, highlighter, nil)
			newCell := oldCell
			oldCell.Number = line.Old
			newCell.Number = line.New
			rows = append(rows, SplitRow{Kind: "line", Old: oldCell, New: newCell})
		}
		index++
	}
	return rows
}

func cellForLine(line document.HunkLine, highlighter *syntaxHighlighter, span *Span) LineCell {
	number := line.New
	if line.Type == "d" {
		number = line.Old
	}
	return LineCell{Number: number, Code: highlighter.highlight(line.Text, span), Class: line.Type}
}

func applyFolds[T any](
	rows []T,
	id string,
	isContext func(T) bool,
	mark func(*T, string, int),
) {
	for start := 0; start < len(rows); {
		if !isContext(rows[start]) {
			start++
			continue
		}
		end := start
		for end < len(rows) && isContext(rows[end]) {
			end++
		}
		if end-start > FoldThreshold {
			foldStart, foldEnd := start+3, end-3
			foldCount := foldEnd - foldStart
			for index := foldStart; index < foldEnd; index++ {
				count := 0
				if index == foldStart {
					count = foldCount
				}
				mark(&rows[index], id, count)
			}
		}
		start = end
	}
}

func isSplitContext(row SplitRow) bool {
	return row.Kind == "line" && row.Old.Class == "c" && row.New.Class == "c"
}

// hunkLabel returns the display text for a hunk separator row. Git headers
// carry the enclosing declaration when available; when they don't, a bare
// "@@" row looks unfinished, so synthesize a line-range label instead.
func hunkLabel(hunk document.Hunk) string {
	if hunk.Header != "" {
		return hunk.Header
	}
	first, last := 0, 0
	for _, line := range hunk.Lines {
		number := line.New
		if number == 0 {
			number = line.Old
		}
		if number == 0 {
			continue
		}
		if first == 0 {
			first = number
		}
		last = number
	}
	if first == 0 {
		return ""
	}
	if first == last {
		return fmt.Sprintf("line %d", first)
	}
	return fmt.Sprintf("lines %d–%d", first, last)
}

func linePrefix(kind string) string {
	switch kind {
	case "a":
		return "+"
	case "d":
		return "-"
	default:
		return " "
	}
}

type syntaxHighlighter struct {
	lexer     chroma.Lexer
	formatter *chromahtml.Formatter
	style     *chroma.Style
}

func newHighlighter(path string) *syntaxHighlighter {
	lexer := lexers.Match(path)
	if lexer == nil {
		lexer = lexers.Fallback
	}
	return &syntaxHighlighter{
		lexer:     chroma.Coalesce(lexer),
		formatter: chromahtml.New(chromahtml.WithClasses(true), chromahtml.PreventSurroundingPre(true)),
		style:     styles.Get("github"),
	}
}

func (h *syntaxHighlighter) highlight(text string, span *Span) template.HTML {
	if span == nil {
		return template.HTML(h.highlightPart(text))
	}
	runes := []rune(text)
	start := min(max(span.Start, 0), len(runes))
	end := min(max(span.End, start), len(runes))
	var output strings.Builder
	output.WriteString(h.highlightPart(string(runes[:start])))
	output.WriteString(`<mark class="word-change">`)
	output.WriteString(h.highlightPart(string(runes[start:end])))
	output.WriteString(`</mark>`)
	output.WriteString(h.highlightPart(string(runes[end:])))
	return template.HTML(output.String())
}

func (h *syntaxHighlighter) highlightPart(text string) string {
	if text == "" {
		return ""
	}
	iterator, err := h.lexer.Tokenise(nil, text)
	if err != nil {
		return template.HTMLEscapeString(text)
	}
	var output bytes.Buffer
	if err := h.formatter.Format(&output, h.style, iterator); err != nil {
		return template.HTMLEscapeString(text)
	}
	return strings.TrimSuffix(output.String(), "\n")
}

type ChromaTheme struct {
	TokenCSS       template.CSS
	LightVariables template.CSS
	DarkVariables  template.CSS
	LightRules     template.CSS
	DarkRules      template.CSS
}

func ChromaThemeCSS(lightStyleName, darkStyleName string) (ChromaTheme, error) {
	lightCSS, err := chromaCSS(lightStyleName)
	if err != nil {
		return ChromaTheme{}, err
	}
	darkCSS, err := chromaCSS(darkStyleName)
	if err != nil {
		return ChromaTheme{}, err
	}
	return buildChromaTheme(lightCSS, darkCSS), nil
}

func chromaCSS(styleName string) (string, error) {
	style := styles.Get(styleName)
	if style == nil {
		return "", fmt.Errorf("chroma style %q not found", styleName)
	}
	formatter := chromahtml.New(chromahtml.WithClasses(true), chromahtml.WithCSSComments(false))
	var output bytes.Buffer
	if err := formatter.WriteCSS(&output, style); err != nil {
		return "", err
	}
	return output.String(), nil
}

type chromaRule struct {
	color        string
	declarations []string
}

var (
	cssRulePattern = regexp.MustCompile(`(?s)([^{}]+)\{([^{}]*)\}`)
	cssVarPattern  = regexp.MustCompile(`[^a-zA-Z0-9]+`)
)

func buildChromaTheme(lightCSS, darkCSS string) ChromaTheme {
	lightRules := parseChromaRules(lightCSS)
	darkRules := parseChromaRules(darkCSS)
	selectors := make([]string, 0, len(lightRules)+len(darkRules))
	seen := map[string]bool{}
	for selector := range lightRules {
		seen[selector] = true
		selectors = append(selectors, selector)
	}
	for selector := range darkRules {
		if !seen[selector] {
			selectors = append(selectors, selector)
		}
	}
	sort.Strings(selectors)

	lightFallback := lightRules[".chroma"].color
	if lightFallback == "" {
		lightFallback = "#1f2328"
	}
	darkFallback := darkRules[".chroma"].color
	if darkFallback == "" {
		darkFallback = "#e6edf3"
	}
	var tokens, lightVariables, darkVariables strings.Builder
	for _, selector := range selectors {
		lightRule := lightRules[selector]
		darkRule := darkRules[selector]
		hasColor := lightRule.color != "" || darkRule.color != ""
		if !hasColor {
			continue
		}
		// Per-line/per-fragment tokenization routinely lexes isolated
		// punctuation as chroma's Error token. Style sheets paint Error as
		// inverse text (white on red); with backgrounds stripped that
		// leaves near-invisible text. Error carries no information here —
		// let those tokens inherit the base text color instead.
		if strings.HasSuffix(selector, ".err") || strings.HasSuffix(selector, ".chroma-err") {
			continue
		}
		variable := "--chroma-" + strings.Trim(cssVarPattern.ReplaceAllString(selector, "-"), "-")
		tokens.WriteString(selector)
		tokens.WriteString(" {")
		tokens.WriteString(" color: var(")
		tokens.WriteString(variable)
		tokens.WriteString(");")
		lightColor := lightRule.color
		if lightColor == "" {
			lightColor = lightFallback
		}
		darkColor := darkRule.color
		if darkColor == "" {
			darkColor = darkFallback
		}
		fmt.Fprintf(&lightVariables, " %s: %s;", variable, lightColor)
		fmt.Fprintf(&darkVariables, " %s: %s;", variable, darkColor)
		tokens.WriteString(" }\n")
	}
	return ChromaTheme{
		TokenCSS:       template.CSS(tokens.String()),
		LightVariables: template.CSS(lightVariables.String()),
		DarkVariables:  template.CSS(darkVariables.String()),
		LightRules:     template.CSS(renderChromaDeclarations(lightRules)),
		DarkRules:      template.CSS(renderChromaDeclarations(darkRules)),
	}
}

func renderChromaDeclarations(rules map[string]chromaRule) string {
	selectors := make([]string, 0, len(rules))
	for selector, rule := range rules {
		if len(rule.declarations) > 0 {
			selectors = append(selectors, selector)
		}
	}
	sort.Strings(selectors)
	var output strings.Builder
	for _, selector := range selectors {
		output.WriteString(selector)
		output.WriteString(" {")
		for _, declaration := range rules[selector].declarations {
			output.WriteByte(' ')
			output.WriteString(declaration)
			output.WriteByte(';')
		}
		output.WriteString(" }\n")
	}
	return output.String()
}

func parseChromaRules(css string) map[string]chromaRule {
	rules := map[string]chromaRule{}
	for _, match := range cssRulePattern.FindAllStringSubmatch(css, -1) {
		selector := strings.TrimSpace(match[1])
		rule := chromaRule{}
		for _, rawDeclaration := range strings.Split(match[2], ";") {
			name, value, ok := strings.Cut(rawDeclaration, ":")
			if !ok {
				continue
			}
			name = strings.TrimSpace(name)
			value = strings.TrimSpace(value)
			switch name {
			case "background", "background-color":
				continue
			case "color":
				rule.color = value
			default:
				if name != "" && value != "" {
					rule.declarations = append(rule.declarations, name+": "+value)
				}
			}
		}
		rules[selector] = rule
	}
	return rules
}
