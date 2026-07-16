package viewer

import (
	"fmt"
	"html/template"
	"strings"
)

const (
	diagramNodeWidth  = 236
	diagramNodeHeight = 62
	diagramGapX       = 64
	diagramGapY       = 22
	diagramPadding    = 10
)

// BuildDiagram renders the cohort-flow diagram as inline SVG built from the
// VALIDATED cohort structure (layers + dependsOn) — never from model
// free-text. Nodes are colored by layer via CSS classes and are clickable
// (data-step-target), so the diagram can never contradict the walkthrough.
func BuildDiagram(steps []StepView) template.HTML {
	var cohorts []StepView
	for _, step := range steps {
		if !step.IsOverview {
			cohorts = append(cohorts, step)
		}
	}
	if len(cohorts) < 2 {
		return ""
	}

	count := len(cohorts)
	depends := make([][]int, count)
	for index, cohort := range cohorts {
		for _, dependency := range cohort.Dependencies {
			target := dependency.StepIndex - 1
			if target >= 0 && target < count && target != index {
				depends[index] = append(depends[index], target)
			}
		}
	}

	// dependsOn is validated to reference strictly earlier cohorts, so a
	// single forward pass computes the dependency depth (diagram column).
	depth := make([]int, count)
	for index := range cohorts {
		for _, target := range depends[index] {
			if depth[target]+1 > depth[index] {
				depth[index] = depth[target] + 1
			}
		}
	}

	columns := map[int][]int{}
	maxDepth := 0
	for index, value := range depth {
		columns[value] = append(columns[value], index)
		if value > maxDepth {
			maxDepth = value
		}
	}
	maxRows := 0
	for _, members := range columns {
		if len(members) > maxRows {
			maxRows = len(members)
		}
	}

	xAt := func(column int) int { return diagramPadding + column*(diagramNodeWidth+diagramGapX) }
	yAt := func(row int) int { return diagramPadding + row*(diagramNodeHeight+diagramGapY) }
	width := xAt(maxDepth) + diagramNodeWidth + diagramPadding
	height := yAt(maxRows-1) + diagramNodeHeight + diagramPadding

	positions := make([][2]int, count)
	for column, members := range columns {
		for row, index := range members {
			positions[index] = [2]int{xAt(column), yAt(row)}
		}
	}

	var svg strings.Builder
	fmt.Fprintf(&svg,
		`<svg class="cohort-diagram" role="img" aria-label="Cohort dependency flow" viewBox="0 0 %d %d">`,
		width, height)
	svg.WriteString(`<defs><marker id="dg-arrow" markerWidth="8" markerHeight="8" refX="7" refY="4" orient="auto"><path class="dg-arrow-head" d="M0,0 L8,4 L0,8 z"/></marker></defs>`)

	for index := range cohorts {
		for _, target := range depends[index] {
			x1 := positions[target][0] + diagramNodeWidth
			y1 := positions[target][1] + diagramNodeHeight/2
			x2 := positions[index][0]
			y2 := positions[index][1] + diagramNodeHeight/2
			middle := (x1 + x2) / 2
			fmt.Fprintf(&svg, `<path class="dg-edge" d="M %d %d C %d %d, %d %d, %d %d"/>`,
				x1, y1, middle, y1, middle, y2, x2, y2)
		}
	}

	for index, cohort := range cohorts {
		x, y := positions[index][0], positions[index][1]
		fmt.Fprintf(&svg, `<g class="dg-node dg-l-%s" data-step-target="%d" tabindex="0" role="link" aria-label="%s">`,
			template.HTMLEscapeString(cohort.Layer), cohort.Index,
			template.HTMLEscapeString(fmt.Sprintf("Go to step %d: %s", cohort.Number, cohort.Title)))
		fmt.Fprintf(&svg, `<rect x="%d" y="%d" width="%d" height="%d" rx="10"/>`,
			x, y, diagramNodeWidth, diagramNodeHeight)
		fmt.Fprintf(&svg, `<text class="dg-title" x="%d" y="%d">%d · %s</text>`,
			x+16, y+26, cohort.Number, template.HTMLEscapeString(truncateRunes(cohort.Title, 26)))
		fmt.Fprintf(&svg, `<text class="dg-sub" x="%d" y="%d">%s · %d %s</text>`,
			x+16, y+45, template.HTMLEscapeString(cohort.Layer), cohort.FileCount, pluralFiles(cohort.FileCount))
		svg.WriteString(`</g>`)
	}
	svg.WriteString(`</svg>`)
	return template.HTML(svg.String())
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit-1]) + "…"
}

func pluralFiles(count int) string {
	if count == 1 {
		return "file"
	}
	return "files"
}
