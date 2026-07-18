package viewer

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func chainSteps(n int) []StepView {
	steps := []StepView{{Index: 0, IsOverview: true}}
	for i := 1; i <= n; i++ {
		step := StepView{Index: i, Number: i, Title: fmt.Sprintf("Step %d", i), Layer: "backend", FileCount: 1}
		if i > 1 {
			step.Dependencies = []DependencyView{{Title: "prev", StepIndex: i - 1}}
		}
		steps = append(steps, step)
	}
	return steps
}

func svgRects(svg string) [][4]int {
	var rects [][4]int
	for _, m := range regexp.MustCompile(`<rect x="(-?\d+)" y="(-?\d+)" width="(\d+)" height="(\d+)"`).FindAllStringSubmatch(svg, -1) {
		x, _ := strconv.Atoi(m[1])
		y, _ := strconv.Atoi(m[2])
		w, _ := strconv.Atoi(m[3])
		h, _ := strconv.Atoi(m[4])
		rects = append(rects, [4]int{x, y, w, h})
	}
	return rects
}

func TestLongChainRendersVertically(t *testing.T) {
	svg := string(BuildDiagram(chainSteps(6)))
	var w, h int
	if _, err := fmt.Sscanf(svg[strings.Index(svg, `width="`):], `width="%d" height="%d"`, &w, &h); err != nil {
		t.Fatal(err)
	}
	if h <= w {
		t.Fatalf("6-step chain should render taller than wide, got %dx%d", w, h)
	}
	rects := svgRects(svg)
	xs := map[int]bool{}
	for _, r := range rects {
		xs[r[0]] = true
	}
	if len(xs) != 1 {
		t.Fatalf("vertical chain should occupy one column, got %d x-positions", len(xs))
	}
}

func TestDisconnectedStepSitsBelowTheFlow(t *testing.T) {
	steps := chainSteps(6)
	steps = append(steps, StepView{Index: 7, Number: 7, Title: "Other changes", Layer: "other", FileCount: 1})
	svg := string(BuildDiagram(steps))
	rects := svgRects(svg)
	if len(rects) != 7 {
		t.Fatalf("expected 7 nodes, got %d", len(rects))
	}
	orphan := rects[6]
	maxConnectedBottom := 0
	for _, r := range rects[:6] {
		maxConnectedBottom = max(maxConnectedBottom, r[1]+r[3])
	}
	if orphan[1] < maxConnectedBottom+diagramSectionGap {
		t.Fatalf("disconnected step at y=%d is not clearly below the flow (bottom %d)", orphan[1], maxConnectedBottom)
	}
}

func TestBarycenterKeepsChildrenNearParents(t *testing.T) {
	// Parents A(1),B(2) in layer 0; C depends on B, D depends on A. Without
	// barycenter, C (step 3) would sit in row 0 crossing both edges.
	steps := []StepView{
		{Index: 0, IsOverview: true},
		{Index: 1, Number: 1, Title: "A", Layer: "backend", FileCount: 1},
		{Index: 2, Number: 2, Title: "B", Layer: "backend", FileCount: 1},
		{Index: 3, Number: 3, Title: "C", Layer: "tests", FileCount: 1,
			Dependencies: []DependencyView{{Title: "B", StepIndex: 2}}},
		{Index: 4, Number: 4, Title: "D", Layer: "tests", FileCount: 1,
			Dependencies: []DependencyView{{Title: "A", StepIndex: 1}}},
	}
	svg := string(BuildDiagram(steps))
	rects := svgRects(svg)
	// order emitted: A, B, C, D. A row must equal D row; B row must equal C row.
	if rects[0][1] != rects[3][1] || rects[1][1] != rects[2][1] {
		t.Fatalf("children not aligned with parents: A=%v B=%v C=%v D=%v", rects[0], rects[1], rects[2], rects[3])
	}
}

func TestSkipEdgesRouteOutsideTheNodeField(t *testing.T) {
	// 3 layers, horizontal (cols=3 is not > 3): 1 -> 2 -> 3 plus skip 1 -> 3.
	steps := []StepView{
		{Index: 0, IsOverview: true},
		{Index: 1, Number: 1, Title: "A", Layer: "backend", FileCount: 1},
		{Index: 2, Number: 2, Title: "B", Layer: "backend", FileCount: 1,
			Dependencies: []DependencyView{{Title: "A", StepIndex: 1}}},
		{Index: 3, Number: 3, Title: "C", Layer: "tests", FileCount: 1,
			Dependencies: []DependencyView{{Title: "B", StepIndex: 2}, {Title: "A", StepIndex: 1}}},
	}
	svg := string(BuildDiagram(steps))
	rects := svgRects(svg)
	nodeBottom := 0
	for _, r := range rects {
		nodeBottom = max(nodeBottom, r[1]+r[3])
	}
	// The skip edge is the corridor path (contains Q segments). Its channel
	// y must be below every node.
	var channelYs []int
	for _, m := range regexp.MustCompile(`L (\d+) (\d+) Q \d+ \d+, \d+ \d+ L \d+ \d+ Q`).FindAllStringSubmatch(svg, -1) {
		y, _ := strconv.Atoi(m[2])
		channelYs = append(channelYs, y)
	}
	found := false
	for _, y := range channelYs {
		if y > nodeBottom {
			found = true
		}
	}
	if !strings.Contains(svg, "Q") {
		t.Fatalf("expected a corridor-routed skip edge: %s", svg)
	}
	if !found {
		t.Fatalf("no corridor segment found below the node field (bottom %d): %s", nodeBottom, svg)
	}
}
