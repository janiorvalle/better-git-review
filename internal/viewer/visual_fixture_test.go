package viewer

import (
	"fmt"
	"os"
	"testing"
)

func TestWriteVisualFixtures(t *testing.T) {
	out := os.Getenv("BGR_VISUAL_FIXTURES_OUT")
	if out == "" {
		t.Skip("set BGR_VISUAL_FIXTURES_OUT to write visual fixtures")
	}
	dep := func(i int) []DependencyView { return []DependencyView{{Title: "d", StepIndex: i}} }
	fundfit := chainSteps(6)
	fundfit = append(fundfit, StepView{Index: 7, Number: 7, Title: "Other changes", Layer: "other", FileCount: 1})
	practico := []StepView{
		{Index: 0, IsOverview: true},
		{Index: 1, Number: 1, Title: "Shared money primitives", Layer: "backend", FileCount: 1},
		{Index: 2, Number: 2, Title: "Rounding and document-total callers", Layer: "backend", FileCount: 14, Dependencies: dep(1)},
		{Index: 3, Number: 3, Title: "Canonical invoice balance", Layer: "backend", FileCount: 6,
			Dependencies: []DependencyView{{Title: "d", StepIndex: 1}, {Title: "d", StepIndex: 4}}},
		{Index: 4, Number: 4, Title: "Shared AI credit billing", Layer: "backend", FileCount: 4},
		{Index: 5, Number: 5, Title: "Equivalence and invariant tests", Layer: "tests", FileCount: 3,
			Dependencies: []DependencyView{{Title: "d", StepIndex: 2}, {Title: "d", StepIndex: 3}, {Title: "d", StepIndex: 1}}},
		{Index: 6, Number: 6, Title: "Plan and handoff documentation", Layer: "docs", FileCount: 2, Dependencies: dep(5)},
	}
	horizontalSkip := []StepView{
		{Index: 0, IsOverview: true},
		{Index: 1, Number: 1, Title: "Schema", Layer: "schema", FileCount: 2},
		{Index: 2, Number: 2, Title: "Backend", Layer: "backend", FileCount: 5, Dependencies: dep(1)},
		{Index: 3, Number: 3, Title: "API", Layer: "api", FileCount: 3, Dependencies: dep(1)},
		{Index: 4, Number: 4, Title: "Tests", Layer: "tests", FileCount: 4,
			Dependencies: []DependencyView{{Title: "d", StepIndex: 2}, {Title: "d", StepIndex: 1}}},
	}
	html := `<!doctype html><meta charset="utf-8"><body style="background:#0d1117;padding:30px;font-family:sans-serif">
<style>
.cohort-diagram{display:block;max-width:100%;height:auto;margin:0 0 40px}
.dg-edge{stroke:#444c56;stroke-width:1.5;fill:none;marker-end:url(#dg-arrow)}
.dg-arrow-head{fill:#444c56}
.dg-node rect{fill:#161b22;stroke:#58a6ff;stroke-width:1.5}
.dg-node text{font:600 13px sans-serif;fill:#e6edf3}
.dg-node .dg-sub{font-weight:400;font-size:11px;fill:#8b949e}
h2{color:#e6edf3;font-size:14px}
</style>`
	for _, fixture := range []struct {
		name  string
		steps []StepView
	}{{"fundfit: 6-chain + orphan", fundfit}, {"practico: dag with skips", practico}, {"horizontal with skip", horizontalSkip}} {
		html += fmt.Sprintf("<h2>%s</h2>%s", fixture.name, string(BuildDiagram(fixture.steps)))
	}
	if err := os.WriteFile(out, []byte(html), 0o644); err != nil {
		t.Fatal(err)
	}
}
