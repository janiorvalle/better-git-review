package viewer

import (
	"bytes"
	"fmt"
	"html/template"
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/alecthomas/chroma/v2"
	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/janiorvalle/better-git-review/internal/document"
)

const (
	FoldThreshold     = 10
	LongLineThreshold = 4_096
)

var unifiedRowsTemplate = template.Must(template.New("unified-rows").Parse(`
<div class="view-unified diff-scroll" data-unified-ready="true">
<table class="diff-table" aria-label="Unified diff for {{.Path}}">
<colgroup><col class="number-column"><col class="number-column"><col class="prefix-column"><col></colgroup><tbody>
{{range $row := .Rows}}
{{if and $row.FoldCount $row.Hidden}}<tr class="fold-row" data-row-kind="fold" data-fold-control="{{$row.FoldID}}"><td colspan="4"><button type="button" class="fold-button" data-fold-target="{{$row.FoldID}}">↕ {{$row.FoldCount}} unmodified lines</button></td></tr>{{end}}
{{if eq $row.Kind "hunk"}}<tr class="hunk-row" data-row-kind="hunk"><td colspan="4">@@ {{$row.Header}}</td></tr>
{{else if eq $row.Kind "blame"}}<tr class="blame-row" data-row-kind="blame"><td colspan="4"><span class="blame-dot" aria-hidden="true"></span><span class="blame-author">{{$row.Blame.Author}}</span> · {{$row.Blame.Date}}</td></tr>
{{else}}<tr class="line-{{$row.Class}}{{if $row.Hidden}} fold-hidden{{end}}" data-row-kind="line" data-line-class="{{$row.Class}}" data-old="{{$row.Old}}" data-new="{{$row.New}}"{{if $row.LongLine}} data-long-line="true"{{end}}{{if $row.Hidden}} data-fold-group="{{$row.FoldID}}"{{end}}><td class="line-number">{{if $row.Old}}{{$row.Old}}{{end}}</td><td class="line-number">{{if $row.New}}{{$row.New}}{{end}}</td><td class="line-prefix">{{$row.Prefix}}</td><td class="code{{if not $row.LongLine}} chroma{{end}}">{{if $row.LongLine}}<span class="long-line-note">long line</span>{{end}}{{$row.Code}}</td></tr>{{end}}
{{end}}
</tbody></table></div>`))

type Span struct {
	Start int
	End   int
}

type UnifiedRow struct {
	Kind      string
	Class     string
	Old       int
	New       int
	Prefix    string
	Code      template.HTML
	LongLine  bool
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

// changedSpanMinSimilarity gates intra-line marks: when the common prefix
// and suffix cover less than this share of the shorter line, the pair is a
// rewrite rather than an edit, and marks would only mislead (GitHub skips
// them in the same situation).
const changedSpanMinSimilarity = 0.5

func ChangedSpans(oldText, newText string) (Span, Span, bool) {
	return ChangedSpansWithSettings(oldText, newText, DefaultSettings())
}

func ChangedSpansWithSettings(oldText, newText string, settings Settings) (Span, Span, bool) {
	if isLongLineWithThreshold(oldText, settings.LongLineThreshold) || isLongLineWithThreshold(newText, settings.LongLineThreshold) {
		return Span{}, Span{}, false
	}
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

	// Similarity is measured over non-whitespace content: shared
	// indentation must not make unrelated lines look like edits of each
	// other. (Whitespace-only lines are trivially similar.)
	commonContent := countNonSpace(oldRunes[:prefix]) + countNonSpace(oldRunes[len(oldRunes)-suffix:])
	shorterContent := min(countNonSpace(oldRunes), countNonSpace(newRunes))
	if shorterContent > 0 && float64(commonContent)/float64(shorterContent) < settings.WordDiffMinSimilarity {
		return Span{}, Span{}, false
	}

	// Snap mark boundaries outward to token edges so marks never start or
	// end mid-identifier (".an|yRequest" reads as noise; ".anyRequest()"
	// reads as a change). The prefix/suffix regions are identical across
	// both lines, so widening keeps the spans aligned.
	for prefix > 0 && isWordRune(oldRunes[prefix-1]) &&
		((prefix < len(oldRunes) && isWordRune(oldRunes[prefix])) ||
			(prefix < len(newRunes) && isWordRune(newRunes[prefix]))) {
		prefix--
	}
	for suffix > 0 {
		suffixStart := oldRunes[len(oldRunes)-suffix]
		lastOld := len(oldRunes) - suffix - 1
		lastNew := len(newRunes) - suffix - 1
		touchesWord := (lastOld >= prefix && lastOld >= 0 && isWordRune(oldRunes[lastOld])) ||
			(lastNew >= prefix && lastNew >= 0 && isWordRune(newRunes[lastNew]))
		if isWordRune(suffixStart) && touchesWord {
			suffix--
			continue
		}
		break
	}

	return Span{Start: prefix, End: len(oldRunes) - suffix},
		Span{Start: prefix, End: len(newRunes) - suffix}, true
}

func isWordRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

func countNonSpace(runes []rune) int {
	count := 0
	for _, r := range runes {
		if !unicode.IsSpace(r) {
			count++
		}
	}
	return count
}

func BuildRows(file document.File, fileIndex int) []UnifiedRow {
	return BuildRowsWithSettings(file, fileIndex, DefaultSettings())
}

func BuildRowsWithSettings(file document.File, fileIndex int, settings Settings) []UnifiedRow {
	highlighter := newHighlighter(file.Path)
	var unified []UnifiedRow
	for hunkIndex, hunk := range file.Hunks {
		if hunk.Blame != nil {
			unified = append(unified, UnifiedRow{Kind: "blame", Blame: hunk.Blame})
		}
		header := hunkLabel(hunk)
		unified = append(unified, UnifiedRow{Kind: "hunk", Header: header})

		pairs := pairChangedLinesWithSettings(hunk.Lines, settings)
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
				Kind:     "line",
				Class:    line.Type,
				Old:      line.Old,
				New:      line.New,
				Prefix:   linePrefix(line.Type),
				Code:     highlighter.highlightWithThreshold(line.Text, span, settings.LongLineThreshold),
				LongLine: isLongLineWithThreshold(line.Text, settings.LongLineThreshold),
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
			settings.FoldThreshold,
			settings.FoldContext,
		)

	}
	return unified
}

func pairChangedLines(lines []document.HunkLine) map[int]linePair {
	return pairChangedLinesWithSettings(lines, DefaultSettings())
}

func pairChangedLinesWithSettings(lines []document.HunkLine, settings Settings) map[int]linePair {
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
			oldText := lines[deleteStart+offset].Text
			newText := lines[addStart+offset].Text
			if isLongLineWithThreshold(oldText, settings.LongLineThreshold) || isLongLineWithThreshold(newText, settings.LongLineThreshold) {
				continue
			}
			oldSpan, newSpan, changed := ChangedSpansWithSettings(oldText, newText, settings)
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

func applyFolds[T any](
	rows []T,
	id string,
	isContext func(T) bool,
	mark func(*T, string, int),
	thresholds ...int,
) {
	foldThreshold := FoldThreshold
	foldContext := DefaultSettings().FoldContext
	if len(thresholds) > 0 {
		foldThreshold = thresholds[0]
	}
	if len(thresholds) > 1 {
		foldContext = thresholds[1]
	}
	for start := 0; start < len(rows); {
		if !isContext(rows[start]) {
			start++
			continue
		}
		end := start
		for end < len(rows) && isContext(rows[end]) {
			end++
		}
		if end-start > foldThreshold {
			foldStart, foldEnd := start+foldContext, end-foldContext
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

var (
	lexerCache       sync.Map
	sharedFormatter  = chromahtml.New(chromahtml.WithClasses(true), chromahtml.PreventSurroundingPre(true))
	sharedLightStyle = styles.Get("github")
)

func newHighlighter(path string) *syntaxHighlighter {
	lexer := lexers.Match(path)
	if lexer == nil {
		lexer = lexers.Fallback
	}
	key := lexer.Config().Name
	cached, ok := lexerCache.Load(key)
	if !ok {
		cached, _ = lexerCache.LoadOrStore(key, chroma.Coalesce(lexer))
	}
	return &syntaxHighlighter{
		lexer:     cached.(chroma.Lexer),
		formatter: sharedFormatter,
		style:     sharedLightStyle,
	}
}

func (h *syntaxHighlighter) highlight(text string, span *Span) template.HTML {
	return h.highlightWithThreshold(text, span, LongLineThreshold)
}

func (h *syntaxHighlighter) highlightWithThreshold(text string, span *Span, threshold int) template.HTML {
	if isLongLineWithThreshold(text, threshold) {
		return template.HTML(template.HTMLEscapeString(text))
	}
	if span == nil || span.Start >= span.End {
		// No span, or an empty span (pure insertion/deletion on the other
		// side) — an empty <mark> adds nothing but DOM noise.
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

func isLongLine(text string) bool {
	return isLongLineWithThreshold(text, LongLineThreshold)
}

func isLongLineWithThreshold(text string, threshold int) bool {
	if len(text) <= threshold {
		return false
	}
	return utf8.RuneCountInString(text) > threshold
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
