package viewer

import (
	"fmt"
	"strings"
	"testing"

	"github.com/janiorvalle/better-git-review/internal/document"
)

func TestBuildDiagramFromValidatedCohorts(t *testing.T) {
	steps := []StepView{
		{Index: 0, IsOverview: true},
		{Index: 1, Number: 1, Title: "Schema changes", Layer: "schema", FileCount: 2},
		{Index: 2, Number: 2, Title: "Backend logic", Layer: "backend", FileCount: 3,
			Dependencies: []DependencyView{{Title: "Schema changes", StepIndex: 1}}},
		{Index: 3, Number: 3, Title: "Tests", Layer: "tests", FileCount: 1,
			Dependencies: []DependencyView{{Title: "Backend logic", StepIndex: 2}}},
	}
	svg := string(BuildDiagram(steps))
	if strings.Count(svg, `class="dg-node`) != 3 {
		t.Fatalf("expected 3 nodes: %s", svg)
	}
	if strings.Count(svg, `class="dg-edge"`) != 2 {
		t.Fatalf("expected 2 dependency edges: %s", svg)
	}
	for _, expected := range []string{
		`dg-l-schema`, `dg-l-backend`, `dg-l-tests`,
		`data-step-target="1"`, `data-step-target="2"`, `data-step-target="3"`,
		`marker id="dg-arrow"`,
	} {
		if !strings.Contains(svg, expected) {
			t.Fatalf("diagram missing %q", expected)
		}
	}
}

func TestBuildDiagramSkipsTrivialWalkthroughs(t *testing.T) {
	steps := []StepView{
		{Index: 0, IsOverview: true},
		{Index: 1, Number: 1, Title: "Only cohort", Layer: "other", FileCount: 1},
	}
	if svg := BuildDiagram(steps); svg != "" {
		t.Fatalf("single-cohort walkthrough should not render a diagram: %s", svg)
	}
}

func TestBuildDiagramEscapesTitles(t *testing.T) {
	hostile := `<script>alert(1)</script>`
	steps := []StepView{
		{Index: 0, IsOverview: true},
		{Index: 1, Number: 1, Title: hostile, Layer: "backend", FileCount: 1},
		{Index: 2, Number: 2, Title: hostile, Layer: "config", FileCount: 1,
			Dependencies: []DependencyView{{Title: hostile, StepIndex: 1}}},
	}
	svg := string(BuildDiagram(steps))
	if strings.Contains(svg, hostile) {
		t.Fatal("hostile title reached the diagram unescaped")
	}
}

func TestDocIDStability(t *testing.T) {
	doc := document.Document{
		SchemaVersion: document.SchemaVersion,
		Source:        document.Source{Range: "main...HEAD"},
		Files:         []document.File{{Path: "a.go", Additions: 3, Deletions: 1}},
	}
	first := docID(doc)
	if first != docID(doc) {
		t.Fatal("docID must be deterministic")
	}
	changed := doc
	changed.Files = []document.File{{Path: "b.go", Additions: 3, Deletions: 1}}
	if first == docID(changed) {
		t.Fatal("docID must change when file content identity changes")
	}
	if len(first) != 16 {
		t.Fatalf("unexpected docID length: %q", first)
	}
}

func TestLangChip(t *testing.T) {
	cases := []struct {
		path   string
		binary bool
		want   string
	}{
		{"main.go", false, "GO"},
		{"src/App.tsx", false, "TSX"},
		{"a/b/SampleSecurityConfiguration.java", false, "JAVA"},
		{"Makefile", false, "MAKE"},
		{"image.png", true, "BIN"},
		{"Dockerfile", false, "DOCKER"},
	}
	for _, testCase := range cases {
		if got := langChip(testCase.path, testCase.binary); got != testCase.want {
			t.Fatalf("langChip(%q, %v) = %q, want %q", testCase.path, testCase.binary, got, testCase.want)
		}
	}
}

func TestFileStepperWiring(t *testing.T) {
	doc := document.Document{
		SchemaVersion: document.SchemaVersion,
		Source:        document.Source{Title: "t", Range: "r"},
		Files: []document.File{
			{Path: "a.go"}, {Path: "b.go"}, {Path: "c.go"},
		},
		Analysis: document.Analysis{
			Title: "t", Overview: "o",
			Cohorts: []document.Cohort{{
				Title: "one", Layer: "backend", Intent: "i", Narrative: "n",
				Files: []int{2, 0, 1}, FileSummaries: []string{"s2", "s0", "s1"},
				ReviewNotes: []string{}, DependsOn: []int{},
			}},
		},
	}
	page, err := buildPage(doc)
	if err != nil {
		t.Fatal(err)
	}
	// Cohort order is 2, 0, 1 — stepper must follow cohort order, not index order.
	if page.Files[2].StepPosition != 1 || page.Files[2].PrevFile != -1 || page.Files[2].NextFile != 0 {
		t.Fatalf("file 2 stepper wrong: %+v", page.Files[2])
	}
	if page.Files[0].StepPosition != 2 || page.Files[0].PrevFile != 2 || page.Files[0].NextFile != 1 {
		t.Fatalf("file 0 stepper wrong: %+v", page.Files[0])
	}
	if page.Files[1].StepPosition != 3 || page.Files[1].PrevFile != 0 || page.Files[1].NextFile != -1 {
		t.Fatalf("file 1 stepper wrong: %+v", page.Files[1])
	}
	if page.Steps[1].FileList != "2,0,1" {
		t.Fatalf("unexpected FileList: %q", page.Steps[1].FileList)
	}
	if page.Files[1].StepTotal != 3 {
		t.Fatalf("unexpected StepTotal: %d", page.Files[1].StepTotal)
	}
}

func TestChromaThemeSuppressesErrorTokenColor(t *testing.T) {
	theme, err := ChromaThemeCSS("github", "github-dark")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(theme.TokenCSS), ".err {") ||
		strings.Contains(string(theme.LightVariables), "-err:") {
		t.Fatal("Error tokens must inherit the base text color (per-fragment lexing artifacts)")
	}
}

func TestBuildDiagramHasIntrinsicPixelSize(t *testing.T) {
	steps := []StepView{
		{Index: 0, IsOverview: true},
		{Index: 1, Number: 1, Title: "A", Layer: "api", FileCount: 1},
		{Index: 2, Number: 2, Title: "B", Layer: "tests", FileCount: 1,
			Dependencies: []DependencyView{{Title: "A", StepIndex: 1}}},
	}
	svg := string(BuildDiagram(steps))
	if !strings.Contains(svg, `width="`) || !strings.Contains(svg, `height="`) {
		t.Fatalf("svg must carry intrinsic width/height so CSS cannot blow it up: %s", svg)
	}
	// width and viewBox must agree (1 unit = 1 CSS px).
	var width, height, vw, vh int
	if _, err := fmt.Sscanf(svg[strings.Index(svg, `width="`):], `width="%d" height="%d" viewBox="0 0 %d %d"`,
		&width, &height, &vw, &vh); err != nil {
		t.Fatalf("could not parse svg dimensions: %v\n%s", err, svg)
	}
	if width != vw || height != vh {
		t.Fatalf("intrinsic size %dx%d disagrees with viewBox %dx%d", width, height, vw, vh)
	}
}

func TestBuildDiagramWrapsDependencyFreeStepsIntoGrid(t *testing.T) {
	steps := []StepView{{Index: 0, IsOverview: true}}
	for i := 1; i <= 9; i++ {
		steps = append(steps, StepView{Index: i, Number: i, Title: fmt.Sprintf("Step %d", i), Layer: "backend", FileCount: 1})
	}
	svg := string(BuildDiagram(steps))
	if strings.Count(svg, `class="dg-node`) != 9 {
		t.Fatalf("expected 9 nodes: %s", svg)
	}
	if strings.Contains(svg, `class="dg-edge"`) {
		t.Fatalf("dependency-free diagram should have no edges: %s", svg)
	}
	// 9 nodes wrap 3-per-row: three distinct x positions and three distinct y
	// positions, not one 9-deep column.
	xs, ys := map[string]bool{}, map[string]bool{}
	rest := svg
	for {
		at := strings.Index(rest, `<rect x="`)
		if at < 0 {
			break
		}
		rest = rest[at:]
		var x, y int
		if _, err := fmt.Sscanf(rest, `<rect x="%d" y="%d"`, &x, &y); err != nil {
			t.Fatalf("could not parse rect: %v", err)
		}
		xs[fmt.Sprint(x)] = true
		ys[fmt.Sprint(y)] = true
		rest = rest[1:]
	}
	if len(xs) != 3 || len(ys) != 3 {
		t.Fatalf("expected a 3x3 grid, got %d x-positions and %d y-positions", len(xs), len(ys))
	}
}
