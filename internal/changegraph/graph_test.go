package changegraph

import (
	"slices"
	"testing"

	"github.com/janiorvalle/better-git-review/internal/document"
)

func TestBuildResolvesTypeScriptRelativeAndIndexImports(t *testing.T) {
	files := []document.File{
		testFile("src/z-definition.ts", `export function calculateTotal() { return 1 }`),
		testFile("src/a-consumer.ts", `import { calculateTotal } from "./z-definition"`, `calculateTotal()`),
		testFile("src/lib/index.ts", `export const sharedValue = 1`),
		testFile("src/b-index-consumer.ts", `const lib = require("./lib")`),
		testFile("src/reexport.ts", `export { sharedValue } from "./lib"`),
		testFile("src/extension-consumer.ts", `import { calculateTotal } from "./z-definition.js"`),
	}
	want := []Edge{
		{Importer: 1, Imported: 0},
		{Importer: 3, Imported: 2},
		{Importer: 4, Imported: 2},
		{Importer: 5, Imported: 0},
	}
	if got := Build(files); !slices.Equal(got, want) {
		t.Fatalf("edges = %#v, want %#v", got, want)
	}
}

func TestBuildResolvesModernTypeScriptModuleExtensions(t *testing.T) {
	files := []document.File{
		testFile("src/helper.mts", `export const abc = 1`),
		testFile("src/consumer.cts", `import { abc } from "./helper.mjs"`),
	}
	want := []Edge{{Importer: 1, Imported: 0}}
	if got := Build(files); !slices.Equal(got, want) {
		t.Fatalf("edges = %#v, want %#v", got, want)
	}
}

func TestBuildDropsAmbiguousExactModuleAlias(t *testing.T) {
	files := []document.File{
		testFile("src/lib.ts", `export const abc = 1`),
		testFile("src/lib/index.ts", `export const abc = 2`),
		testFile("src/consumer.ts", `import { abc } from "./lib"`),
	}
	if got := Build(files); len(got) != 0 {
		t.Fatalf("ambiguous exact alias produced edges: %#v", got)
	}
}

func TestBuildResolvesMultilineTypeScriptImportAndIgnoresCommentOnlyImport(t *testing.T) {
	files := []document.File{
		testFile("src/helper.ts", `export const abc = 1`),
		testFile("src/consumer.ts", `import {`, `  abc`, `} from "./helper"`),
		testFile("src/comment.ts", `// import { abc } from "./helper"`,
			`// const fake = require("./helper")`, `const note = 'require("./helper")'`),
	}
	want := []Edge{{Importer: 1, Imported: 0}}
	if got := Build(files); !slices.Equal(got, want) {
		t.Fatalf("edges = %#v, want %#v", got, want)
	}
}

func TestBuildDoesNotLeakSemicolonlessJSImportState(t *testing.T) {
	files := []document.File{
		testFile("src/helper.ts", `export const abc = 1`),
		testFile("src/polyfill.ts", `export const xyz = 1`),
		testFile("src/side-effect.ts", `import "./polyfill"`, `const note = 'from "./helper"'`),
		testFile("src/local-export.ts", `export { abc }`, `const note = 'from "./helper"'`),
	}
	if got := Build(files); len(got) != 0 {
		t.Fatalf("semicolonless statement leaked parser state: %#v", got)
	}
}

func TestBuildResolvesGoPackageImport(t *testing.T) {
	files := []document.File{
		testFile("internal/money/money.go", `package money`, `func CalculateTotal() int { return 1 }`),
		testFile("cmd/app/main.go", `package main`, `import "github.com/acme/project/internal/money"`, `var total = money.CalculateTotal()`),
	}
	want := []Edge{{Importer: 1, Imported: 0}}
	if got := Build(files); !slices.Equal(got, want) {
		t.Fatalf("edges = %#v, want %#v", got, want)
	}
}

func TestBuildDropsAmbiguousRepositorySuffix(t *testing.T) {
	files := []document.File{
		testFile("one/money/money.go", `package money`),
		testFile("two/money/money.go", `package money`),
		testFile("cmd/app/main.go", `package main`, `import "money"`),
	}
	if got := Build(files); len(got) != 0 {
		t.Fatalf("ambiguous import produced edges: %#v", got)
	}
}

func TestBuildResolvesPythonModule(t *testing.T) {
	files := []document.File{
		testFile("pkg/money.py", `def calculate_total():`, `    return 1`),
		testFile("pkg/service.py", `from .money import calculate_total`, `value = calculate_total()`),
		testFile("pkg/runner.py", `import pkg.money`),
	}
	want := []Edge{{Importer: 1, Imported: 0}, {Importer: 2, Imported: 0}}
	if got := Build(files); !slices.Equal(got, want) {
		t.Fatalf("edges = %#v, want %#v", got, want)
	}
}

func TestBuildResolvesEveryCommaSeparatedPythonImport(t *testing.T) {
	files := []document.File{
		testFile("pkg/alpha.py", `value = 1`),
		testFile("pkg/beta.py", `value = 2`),
		testFile("pkg/consumer.py", `import pkg.alpha, pkg.beta as beta`),
	}
	want := []Edge{{Importer: 2, Imported: 0}, {Importer: 2, Imported: 1}}
	if got := Build(files); !slices.Equal(got, want) {
		t.Fatalf("edges = %#v, want %#v", got, want)
	}
}

func TestBuildRejectsRelativeImportOutsideRepository(t *testing.T) {
	files := []document.File{
		testFile("shared.ts", `export const abc = 1`),
		testFile("src/consumer.ts", `import { abc } from "../../shared"`),
	}
	if got := Build(files); len(got) != 0 {
		t.Fatalf("out-of-repository import produced edges: %#v", got)
	}
}

func TestBuildRejectsPythonRelativeImportAboveRepository(t *testing.T) {
	files := []document.File{
		testFile("shared.py", `value = 1`),
		testFile("service.py", `from ..shared import value`),
		testFile("pkg/service.py", `from ...shared import value`),
	}
	if got := Build(files); len(got) != 0 {
		t.Fatalf("out-of-repository Python import produced edges: %#v", got)
	}
}

func TestBuildAllowsPythonRelativeImportToRepositoryRoot(t *testing.T) {
	files := []document.File{
		testFile("shared.py", `value = 1`),
		testFile("pkg/service.py", `from ..shared import value`),
	}
	want := []Edge{{Importer: 1, Imported: 0}}
	if got := Build(files); !slices.Equal(got, want) {
		t.Fatalf("edges = %#v, want %#v", got, want)
	}
}

func TestBuildFindsLanguageSpecificExportedDefinitions(t *testing.T) {
	files := []document.File{
		testFile("go/definition.go", `func GoHelper() {}`),
		testFile("go/consumer.go", `func use() { GoHelper() }`),
		testFile("python/definition.py", `class PythonHelper:`),
		testFile("python/consumer.py", `value = PythonHelper()`),
		testFile("typescript/definition.ts", `export type SharedType = string`),
		testFile("typescript/consumer.ts", `const value: SharedType = "x"`),
	}
	want := []Edge{
		{Importer: 1, Imported: 0},
		{Importer: 3, Imported: 2},
		{Importer: 5, Imported: 4},
	}
	if got := Build(files); !slices.Equal(got, want) {
		t.Fatalf("edges = %#v, want %#v", got, want)
	}
}

func TestBuildUsesOnlyUniqueSymbolsWithAtLeastFourCharacters(t *testing.T) {
	files := []document.File{
		testFile("a.ts", `export function SharedName() {}`, `export function Foo() {}`),
		testFile("b.ts", `export class SharedName {}`),
		testFile("c.ts", `SharedName()`, `Foo()`),
		testFile("d.ts", `export interface UniqueType { value: string }`),
		testFile("e.ts", `const value: UniqueType = { value: "x" }`),
	}
	want := []Edge{{Importer: 4, Imported: 3}}
	if got := Build(files); !slices.Equal(got, want) {
		t.Fatalf("edges = %#v, want %#v", got, want)
	}
}

func TestBuildIgnoresContextAndDeletedLines(t *testing.T) {
	definition := testFile("src/definition.ts", `export const sharedValue = 1`)
	consumer := document.File{
		Path: "src/consumer.ts",
		Hunks: []document.Hunk{{Lines: []document.HunkLine{
			{Type: "c", Text: `import { sharedValue } from "./definition"`},
			{Type: "d", Text: `sharedValue`},
		}}},
	}
	if got := Build([]document.File{definition, consumer}); len(got) != 0 {
		t.Fatalf("non-added lines produced edges: %#v", got)
	}
}

func TestStableOrderPutsDependenciesFirstAndKeepsEdgeFreeOrder(t *testing.T) {
	if got := StableOrder([]int{0, 1}, []Edge{{Importer: 0, Imported: 1}}); !slices.Equal(got, []int{1, 0}) {
		t.Fatalf("ordered = %#v", got)
	}
	current := []int{3, 1, 2}
	if got := StableOrder(current, nil); !slices.Equal(got, current) {
		t.Fatalf("edge-free order changed: %#v", got)
	}
}

func TestStableOrderDropsCycleEdges(t *testing.T) {
	current := []int{1, 0, 2}
	edges := []Edge{
		{Importer: 0, Imported: 1},
		{Importer: 1, Imported: 0},
		{Importer: 2, Imported: 0},
	}
	if got := StableOrder(current, edges); !slices.Equal(got, current) {
		t.Fatalf("cycle members did not retain current order: %#v", got)
	}
}

func TestStableOrderKeepsCycleMemberOrderWithExternalDependency(t *testing.T) {
	current := []int{1, 0, 2}
	edges := []Edge{
		{Importer: 0, Imported: 1},
		{Importer: 1, Imported: 0},
		{Importer: 1, Imported: 2},
	}
	if got := StableOrder(current, edges); !slices.Equal(got, []int{2, 1, 0}) {
		t.Fatalf("cycle member order changed around external dependency: %#v", got)
	}
}

func TestCohortDependenciesKeepsTopThreeEarlierInbound(t *testing.T) {
	cohorts := [][]int{{0, 1}, {10, 11}, {20}, {30}, {40, 41}}
	edges := []Edge{
		{Importer: 40, Imported: 0}, {Importer: 41, Imported: 1},
		{Importer: 40, Imported: 10}, {Importer: 41, Imported: 11},
		{Importer: 40, Imported: 20}, {Importer: 40, Imported: 30},
		{Importer: 10, Imported: 40},
		{Importer: 40, Imported: 41},
	}
	got := CohortDependencies(cohorts, edges)
	if !slices.Equal(got[4], []int{0, 1, 2}) {
		t.Fatalf("top inbound dependencies = %#v", got[4])
	}
	for index := 0; index < 4; index++ {
		if len(got[index]) != 0 {
			t.Fatalf("cohort %d dependencies = %#v", index, got[index])
		}
	}
}

func testFile(path string, lines ...string) document.File {
	hunkLines := make([]document.HunkLine, len(lines))
	for index, line := range lines {
		hunkLines[index] = document.HunkLine{Type: "a", Text: line}
	}
	return document.File{
		Path: path, Additions: len(lines),
		Hunks: []document.Hunk{{Lines: hunkLines}},
	}
}
