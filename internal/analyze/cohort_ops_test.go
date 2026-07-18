package analyze

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/janiorvalle/better-git-review/internal/changegraph"
	"github.com/janiorvalle/better-git-review/internal/document"
)

func TestApplyCohortOpsResolvesChainedMergesAndRebuildsOrdering(t *testing.T) {
	files := []document.File{
		{Path: "src/a-consumer.ts"},
		{Path: "src/z-definition.ts"},
		{Path: "src/extra.ts"},
		{Path: "app/consumer.ts"},
	}
	settings := DefaultSettings()
	plan := StagedPlan{
		Cohorts: []PlannedCohort{
			{Title: "Dependent", Layer: "backend", Files: []int{3}},
			{Title: "Target", Layer: "backend", Files: []int{0}},
			{Title: "Middle", Layer: "backend", Files: []int{1}},
			{Title: "Tail", Layer: "backend", Files: []int{2}},
		},
		Calls: 99,
		edges: []changegraph.Edge{
			{Importer: 0, Imported: 1},
			{Importer: 3, Imported: 2},
		},
		settings: settings,
	}
	narrations := []CohortNarration{
		{Title: "Dependent", Intent: "dependent", Narrative: "dependent narrative", ReviewNotes: []string{"dependent"}},
		{Title: "Target", Intent: "target", Narrative: "target narrative", ReviewNotes: []string{"shared", "target"}},
		{Title: "Middle", Intent: "middle", Narrative: "middle narrative", ReviewNotes: []string{"shared", "middle"}},
		{Title: "Tail", Intent: "tail", Narrative: "tail narrative", ReviewNotes: []string{"tail"}},
	}
	var logs []string
	refined, refinedNarrations, stubbed := ApplyCohortOps(
		files,
		plan,
		narrations,
		[]int{0, 2},
		[]CohortOp{
			{Op: "merge", Into: intPointer(1), From: intPointer(2)},
			{Op: "merge", Into: intPointer(2), From: intPointer(3)},
			{Op: "retitle", Cohort: intPointer(2), Title: "Merged once"},
			{Op: "retitle", Cohort: intPointer(1), Title: "Merged final"},
			{Op: "merge", Into: intPointer(0), From: intPointer(2)},
		},
		func(format string, args ...any) { logs = append(logs, fmt.Sprintf(format, args...)) },
	)
	if refined.Calls != plan.Calls {
		t.Fatalf("call count changed: got %d want %d", refined.Calls, plan.Calls)
	}
	if len(refined.Cohorts) != 2 || !slices.Equal(refined.Cohorts[0].Files, []int{1, 0, 2}) ||
		!slices.Equal(refined.Cohorts[1].Files, []int{3}) {
		t.Fatalf("refined cohorts = %#v", refined.Cohorts)
	}
	if refinedNarrations[0].Title != "Merged final" || refinedNarrations[0].Narrative != "target narrative" {
		t.Fatalf("target narration = %#v", refinedNarrations[0])
	}
	if !slices.Equal(refinedNarrations[0].ReviewNotes, []string{"shared", "target", "middle", "tail"}) {
		t.Fatalf("merged review notes = %#v", refinedNarrations[0].ReviewNotes)
	}
	if !slices.Equal(refined.dependencies[1], []int{0}) {
		t.Fatalf("rebuilt dependencies = %#v", refined.dependencies)
	}
	if !slices.Equal(stubbed, []int{0, 1}) {
		t.Fatalf("remapped stubbed cohorts = %#v", stubbed)
	}
	if len(logs) != 1 || !strings.Contains(logs[0], "already merged") {
		t.Fatalf("ignored-op logs = %#v", logs)
	}

	summaries := make([]FileSummary, len(files))
	for index := range summaries {
		summaries[index] = FileSummary{Summary: "summary", KeySymbols: []string{}}
	}
	analysis := AssembleStagedAnalysis(files, refined, summaries, refinedNarrations, Synthesis{
		Title: "Change", Overview: "Overview",
	})
	analysis.StubbedCohorts = stubbed
	if errors := ValidateComplete(analysis, len(files)); len(errors) != 0 {
		t.Fatalf("refined analysis invariants failed: %#v", errors)
	}
}

func TestApplyCohortOpsIgnoresInvalidOpsIndividuallyAndCapsTheList(t *testing.T) {
	settings := DefaultSettings()
	settings.ReadingOrder = false
	settings.StepOrder = false
	plan := StagedPlan{
		Cohorts:  []PlannedCohort{{Title: "Original", Layer: "backend", Files: []int{0}}},
		settings: settings,
	}
	narrations := []CohortNarration{{
		Title: "Original", Intent: "intent", Narrative: "narrative", ReviewNotes: []string{},
	}}
	ops := []CohortOp{
		{Op: "merge", Into: intPointer(0)},
		{Op: "merge", Into: intPointer(0), From: intPointer(0)},
		{Op: "retitle", Cohort: intPointer(9), Title: "bad"},
		{Op: "unknown"},
		{Op: "retitle", Cohort: intPointer(0), Title: "Valid"},
	}
	for len(ops) < MaxCohortOps {
		ops = append(ops, CohortOp{Op: "retitle", Cohort: intPointer(0), Title: "Within cap"})
	}
	ops = append(ops, CohortOp{Op: "retitle", Cohort: intPointer(0), Title: "Beyond cap"})
	var logs []string
	_, refinedNarrations, _ := ApplyCohortOps(
		[]document.File{{Path: "main.go"}}, plan, narrations, nil, ops,
		func(format string, args ...any) { logs = append(logs, fmt.Sprintf(format, args...)) },
	)
	if refinedNarrations[0].Title != "Within cap" {
		t.Fatalf("title = %q", refinedNarrations[0].Title)
	}
	if !slices.ContainsFunc(logs, func(line string) bool { return strings.Contains(line, "operation cap") }) {
		t.Fatalf("missing cap log: %#v", logs)
	}
}

func TestApplyCohortOpsDisabledOrEmptyIsUnchanged(t *testing.T) {
	plan := StagedPlan{
		Cohorts:  []PlannedCohort{{Title: "Original", Layer: "backend", Files: []int{0}}},
		settings: DefaultSettings(),
	}
	narrations := []CohortNarration{{
		Title: "Original", Intent: "intent", Narrative: "narrative", ReviewNotes: []string{},
	}}
	before, _ := json.Marshal(struct {
		Plan       StagedPlan
		Narrations []CohortNarration
	}{plan, narrations})
	emptyPlan, emptyNarrations, _ := ApplyCohortOps(nil, plan, narrations, nil, nil, nil)
	empty, _ := json.Marshal(struct {
		Plan       StagedPlan
		Narrations []CohortNarration
	}{emptyPlan, emptyNarrations})
	if !slices.Equal(before, empty) {
		t.Fatal("empty ops changed staged inputs")
	}
	plan.settings.CohortOps = false
	disabledPlan, disabledNarrations, _ := ApplyCohortOps(
		nil, plan, narrations, nil,
		[]CohortOp{{Op: "retitle", Cohort: intPointer(0), Title: "Changed"}}, nil,
	)
	if disabledPlan.Cohorts[0].Title != "Original" || disabledNarrations[0].Title != "Original" {
		t.Fatal("disabled cohort ops changed staged inputs")
	}
}

func TestSynthesisDecodesMalformedCohortOpsIndividually(t *testing.T) {
	var synthesis Synthesis
	err := json.Unmarshal([]byte(`{
		"title":"Change",
		"overview":"Overview",
		"cohortOps":[
			{"op":"retitle","cohort":0,"title":"First"},
			{"op":"merge","into":"zero","from":1},
			{"op":"retitle","cohort":0,"title":"Last"}
		]
	}`), &synthesis)
	if err != nil {
		t.Fatalf("one malformed operation failed synthesis decoding: %v", err)
	}
	if len(synthesis.CohortOps) != 3 || synthesis.CohortOps[1].Invalid == "" {
		t.Fatalf("decoded operations = %#v", synthesis.CohortOps)
	}
	plan := StagedPlan{
		Cohorts:  []PlannedCohort{{Title: "Original", Files: []int{0}}},
		settings: DefaultSettings(),
	}
	narrations := []CohortNarration{{Title: "Original"}}
	var logs []string
	_, refined, _ := ApplyCohortOps(
		[]document.File{{Path: "main.go"}}, plan, narrations, nil, synthesis.CohortOps,
		func(format string, args ...any) { logs = append(logs, fmt.Sprintf(format, args...)) },
	)
	if refined[0].Title != "Last" {
		t.Fatalf("valid operations around malformed item were not applied: %#v", refined)
	}
	if len(logs) != 1 || !strings.Contains(logs[0], "invalid value") {
		t.Fatalf("malformed operation log = %#v", logs)
	}

	var omitted Synthesis
	if err := json.Unmarshal([]byte(`{"title":"Change","overview":"Overview"}`), &omitted); err != nil {
		t.Fatalf("omitted cohortOps failed non-structured decoding: %v", err)
	}
	if omitted.CohortOps != nil {
		t.Fatalf("omitted cohortOps = %#v", omitted.CohortOps)
	}
}

func TestSynthesisSchemaRequiresNullableCohortOpFieldsForStrictProviders(t *testing.T) {
	var schema schemaNode
	if err := json.Unmarshal(SynthesisSchema, &schema); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(schema.Required, "cohortOps") {
		t.Fatal("strict synthesis schema does not require cohortOps")
	}
	operations := schema.Properties["cohortOps"]
	if operations.Items == nil {
		t.Fatal("cohortOps item schema is missing")
	}
	for _, field := range []string{"op", "into", "from", "cohort", "title"} {
		if !slices.Contains(operations.Items.Required, field) {
			t.Errorf("strict cohort operation schema does not require %q", field)
		}
	}
	var disabled schemaNode
	if err := json.Unmarshal(SynthesisSchemaWithoutCohortOps, &disabled); err != nil {
		t.Fatal(err)
	}
	if _, ok := disabled.Properties["cohortOps"]; ok {
		t.Fatal("disabled synthesis schema still exposes cohortOps")
	}
}

func TestCohortOpUnknownFieldDiagnosticIsDeterministic(t *testing.T) {
	for range 20 {
		var operation CohortOp
		if err := json.Unmarshal([]byte(`{"op":"merge","zeta":1,"alpha":2}`), &operation); err != nil {
			t.Fatal(err)
		}
		if operation.Invalid != `unknown field "alpha"` {
			t.Fatalf("invalid operation diagnostic = %q", operation.Invalid)
		}
	}
}

func intPointer(value int) *int { return &value }
