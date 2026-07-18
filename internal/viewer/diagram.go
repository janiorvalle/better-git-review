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

	layout := layoutDiagram(cohorts, depends, depth)

	var svg strings.Builder
	// width/height give the SVG its intrinsic CSS-pixel size (1 unit = 1px):
	// with only a viewBox it would stretch to the container and a small
	// diagram would blow up to poster scale.
	fmt.Fprintf(&svg,
		`<svg class="cohort-diagram" role="img" aria-label="Cohort dependency flow" width="%d" height="%d" viewBox="0 0 %d %d">`,
		layout.width, layout.height, layout.width, layout.height)
	svg.WriteString(`<defs><marker id="dg-arrow" markerWidth="8" markerHeight="8" refX="7" refY="4" orient="auto"><path class="dg-arrow-head" d="M0,0 L8,4 L0,8 z"/></marker></defs>`)

	for _, edge := range layout.edges {
		fmt.Fprintf(&svg, `<path class="dg-edge" d="%s"/>`, edge)
	}
	positions := layout.positions

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

const (
	diagramLayerGapV  = 46 // vertical distance between layers in vertical orientation
	diagramChannelGap = 44 // clear routing lane reserved for layer-skipping edges
	diagramSectionGap = 40 // separation above the disconnected-steps grid
)

type diagramLayout struct {
	positions [][2]int
	edges     []string
	width     int
	height    int
}

// layoutDiagram places connected cohorts in a layered flow and disconnected
// cohorts in a separate trailing grid. Deep, narrow graphs (long chains)
// render vertically instead of as one endless horizontal strip. Rows within
// a layer are ordered by the mean position of their dependencies (barycenter)
// so children sit near their parents, and edges that skip a layer are routed
// through a reserved channel outside the node field instead of plowing
// through intermediate layers. Everything is deterministic: ties break on
// step number.
func layoutDiagram(cohorts []StepView, depends [][]int, depth []int) diagramLayout {
	count := len(cohorts)
	positions := make([][2]int, count)
	hasEdge := make([]bool, count)
	for index, targets := range depends {
		if len(targets) > 0 {
			hasEdge[index] = true
			for _, target := range targets {
				hasEdge[target] = true
			}
		}
	}
	var connected, orphans []int
	for index := range cohorts {
		if hasEdge[index] {
			connected = append(connected, index)
		} else {
			orphans = append(orphans, index)
		}
	}

	gridAt := func(position, perRow, topY int) [2]int {
		gapX := diagramGapY
		return [2]int{
			diagramPadding + (position%perRow)*(diagramNodeWidth+gapX),
			topY + (position/perRow)*(diagramNodeHeight+diagramGapY),
		}
	}

	// No dependencies anywhere: pure reading-order grid (at most 3 per row).
	if len(connected) == 0 {
		perRow := min(3, count)
		for index := range cohorts {
			positions[index] = gridAt(index, perRow, diagramPadding)
		}
		return diagramLayout{
			positions: positions,
			width:     diagramPadding + perRow*diagramNodeWidth + (perRow-1)*diagramGapY + diagramPadding,
			height:    diagramPadding + ((count-1)/3+1)*diagramNodeHeight + ((count-1)/3)*diagramGapY + diagramPadding,
		}
	}

	// Layer assignment with barycenter row ordering: layer 0 by step number,
	// deeper layers by the mean row of their dependencies, ties by step.
	maxDepth := 0
	for _, index := range connected {
		maxDepth = max(maxDepth, depth[index])
	}
	layers := make([][]int, maxDepth+1)
	for _, index := range connected {
		layers[depth[index]] = append(layers[depth[index]], index)
	}
	rowOf := make([]int, count)
	for layer, members := range layers {
		type placed struct {
			index int
			key   float64
		}
		ordered := make([]placed, len(members))
		for position, index := range members {
			key := float64(index)
			if layer > 0 && len(depends[index]) > 0 {
				sum := 0
				for _, target := range depends[index] {
					sum += rowOf[target]
				}
				key = float64(sum)/float64(len(depends[index])) + float64(index)/float64(count+1)/1000
			}
			ordered[position] = placed{index: index, key: key}
		}
		for i := 1; i < len(ordered); i++ {
			for j := i; j > 0 && (ordered[j].key < ordered[j-1].key ||
				(ordered[j].key == ordered[j-1].key && ordered[j].index < ordered[j-1].index)); j-- {
				ordered[j], ordered[j-1] = ordered[j-1], ordered[j]
			}
		}
		for row, entry := range ordered {
			rowOf[entry.index] = row
		}
		layers[layer] = layers[layer][:0]
		for _, entry := range ordered {
			layers[layer] = append(layers[layer], entry.index)
		}
	}
	maxRows := 0
	for _, members := range layers {
		maxRows = max(maxRows, len(members))
	}

	// Deep, narrow graphs read better top-to-bottom.
	vertical := maxDepth+1 > 3 && maxDepth+1 >= 2*maxRows

	skip := false
	for index, targets := range depends {
		for _, target := range targets {
			if depth[index]-depth[target] > 1 {
				skip = true
			}
		}
	}

	var fieldWidth, fieldHeight int
	if vertical {
		for _, index := range connected {
			positions[index] = [2]int{
				diagramPadding + rowOf[index]*(diagramNodeWidth+diagramGapY),
				diagramPadding + depth[index]*(diagramNodeHeight+diagramLayerGapV),
			}
		}
		fieldWidth = diagramPadding + maxRows*diagramNodeWidth + (maxRows-1)*diagramGapY + diagramPadding
		fieldHeight = diagramPadding + (maxDepth+1)*diagramNodeHeight + maxDepth*diagramLayerGapV + diagramPadding
		if skip {
			fieldWidth += diagramChannelGap
		}
	} else {
		for _, index := range connected {
			positions[index] = [2]int{
				diagramPadding + depth[index]*(diagramNodeWidth+diagramGapX),
				diagramPadding + rowOf[index]*(diagramNodeHeight+diagramGapY),
			}
		}
		fieldWidth = diagramPadding + (maxDepth+1)*diagramNodeWidth + maxDepth*diagramGapX + diagramPadding
		fieldHeight = diagramPadding + maxRows*diagramNodeHeight + (maxRows-1)*diagramGapY + diagramPadding
		if skip {
			fieldHeight += diagramChannelGap
		}
	}

	var edges []string
	for index, targets := range depends {
		for _, target := range targets {
			span := depth[index] - depth[target]
			source, sink := positions[target], positions[index]
			switch {
			case span > 1 && vertical:
				// Corridor route: down into the layer gap below the source,
				// across to the right-hand channel, down the channel, back in
				// through the layer gap above the target, into its top edge.
				// Every segment travels in node-free space.
				x1 := source[0] + diagramNodeWidth/2
				x2 := sink[0] + diagramNodeWidth/2
				gapOut := source[1] + diagramNodeHeight + diagramLayerGapV/2
				gapIn := sink[1] - diagramLayerGapV/2
				channelX := fieldWidth - diagramChannelGap/2
				edges = append(edges, fmt.Sprintf(
					"M %d %d L %d %d Q %d %d, %d %d L %d %d Q %d %d, %d %d L %d %d Q %d %d, %d %d L %d %d Q %d %d, %d %d L %d %d",
					x1, source[1]+diagramNodeHeight,
					x1, gapOut-12, x1, gapOut, x1+12, gapOut,
					channelX-12, gapOut, channelX, gapOut, channelX, gapOut+12,
					channelX, gapIn-12, channelX, gapIn, channelX-12, gapIn,
					x2+12, gapIn, x2, gapIn, x2, gapIn+12,
					x2, sink[1]))
			case span > 1:
				// Corridor route: out into the column gap right of the source,
				// down to the bottom channel, across, up the column gap left
				// of the target, into its left edge.
				y1 := source[1] + diagramNodeHeight/2
				y2 := sink[1] + diagramNodeHeight/2
				gapOut := source[0] + diagramNodeWidth + diagramGapX/2
				gapIn := sink[0] - diagramGapX/2
				channelY := fieldHeight - diagramChannelGap/2
				edges = append(edges, fmt.Sprintf(
					"M %d %d L %d %d Q %d %d, %d %d L %d %d Q %d %d, %d %d L %d %d Q %d %d, %d %d L %d %d Q %d %d, %d %d L %d %d",
					source[0]+diagramNodeWidth, y1,
					gapOut-12, y1, gapOut, y1, gapOut, y1+12,
					gapOut, channelY-12, gapOut, channelY, gapOut+12, channelY,
					gapIn-12, channelY, gapIn, channelY, gapIn, channelY-12,
					gapIn, y2+12, gapIn, y2, gapIn+12, y2,
					sink[0], y2))
			case vertical:
				x1 := source[0] + diagramNodeWidth/2
				y1 := source[1] + diagramNodeHeight
				x2 := sink[0] + diagramNodeWidth/2
				y2 := sink[1]
				middle := (y1 + y2) / 2
				edges = append(edges, fmt.Sprintf("M %d %d C %d %d, %d %d, %d %d",
					x1, y1, x1, middle, x2, middle, x2, y2))
			default:
				x1 := source[0] + diagramNodeWidth
				y1 := source[1] + diagramNodeHeight/2
				x2 := sink[0]
				y2 := sink[1] + diagramNodeHeight/2
				middle := (x1 + x2) / 2
				edges = append(edges, fmt.Sprintf("M %d %d C %d %d, %d %d, %d %d",
					x1, y1, middle, y1, middle, y2, x2, y2))
			}
		}
	}

	width, height := fieldWidth, fieldHeight
	if len(orphans) > 0 {
		// Disconnected steps live in their own grid, clearly below the flow.
		perRow := min(3, len(orphans))
		topY := fieldHeight + diagramSectionGap
		for position, index := range orphans {
			positions[index] = gridAt(position, perRow, topY)
		}
		gridWidth := diagramPadding + perRow*diagramNodeWidth + (perRow-1)*diagramGapY + diagramPadding
		width = max(width, gridWidth)
		height = topY + ((len(orphans)-1)/perRow+1)*diagramNodeHeight + ((len(orphans)-1)/perRow)*diagramGapY + diagramPadding
	}
	return diagramLayout{positions: positions, edges: edges, width: width, height: height}
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
