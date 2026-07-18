package analyze

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/janiorvalle/better-git-review/internal/changegraph"
	"github.com/janiorvalle/better-git-review/internal/document"
	"github.com/janiorvalle/better-git-review/internal/pathlayer"
	"github.com/janiorvalle/better-git-review/internal/provider"
)

const (
	SummaryBatchMaxFiles = 25
	CohortMaxFiles       = 150
	DigestMaxFiles       = 40
	DigestMaxChars       = 60_000
)

type TriageResult struct {
	ReviewWorthy  []int
	Mechanical    []int
	MechanicalWhy map[int]string
	Flags         map[int][]string
}

type PlannedCohort struct {
	Title     string
	Layer     string
	Directory string
	Files     []int
}

type SummaryBatch struct {
	Files      []int
	DiffLimits []int
	InputChars int
}

type StagedPlan struct {
	Triage         TriageResult
	Cohorts        []PlannedCohort
	SummaryBatches []SummaryBatch
	Calls          int
	edges          []changegraph.Edge
	settings       Settings
}

type Settings struct {
	SummaryBatchMaxFiles int
	StageConcurrency     int
	DigestMaxFiles       int
	DigestMaxChars       int
	FileDiffCap          int
	StagingMaxFiles      int
	ReadingOrder         bool
	CohortDependencies   bool
}

func DefaultSettings() Settings {
	return Settings{
		SummaryBatchMaxFiles: SummaryBatchMaxFiles, StageConcurrency: StageConcurrency,
		DigestMaxFiles: DigestMaxFiles, DigestMaxChars: DigestMaxChars,
		FileDiffCap: maxFileDiffCap, StagingMaxFiles: CohortMaxFiles,
		ReadingOrder: true, CohortDependencies: true,
	}
}

func PlanStaged(files []document.File, generated map[int]bool, includeMechanical bool, budget int) StagedPlan {
	return PlanStagedWithSettings(files, generated, includeMechanical, budget, DefaultSettings())
}

func PlanStagedWithSettings(files []document.File, generated map[int]bool, includeMechanical bool, budget int, settings Settings) StagedPlan {
	if budget <= 0 {
		budget = provider.DefaultAnalysisBudget
	}
	triage := Triage(files, generated, includeMechanical)
	cohorts := PlanCohortsWithMax(files, settings.StagingMaxFiles)
	var edges []changegraph.Edge
	if settings.ReadingOrder || settings.CohortDependencies {
		edges = changegraph.Build(files)
	}
	if settings.ReadingOrder {
		for index := range cohorts {
			cohorts[index].Files = changegraph.StableOrder(cohorts[index].Files, edges)
		}
	}
	batches := PlanSummaryBatchesWithSettings(files, triage.ReviewWorthy, budget, settings)
	return StagedPlan{
		Triage:         triage,
		Cohorts:        cohorts,
		SummaryBatches: batches,
		Calls:          len(batches) + len(cohorts) + 1,
		edges:          edges,
		settings:       settings,
	}
}

func Triage(files []document.File, generated map[int]bool, includeMechanical bool) TriageResult {
	result := TriageResult{
		MechanicalWhy: map[int]string{},
		Flags:         map[int][]string{},
	}
	for index, file := range files {
		flags := heuristicFlags(file)
		if len(flags) > 0 {
			result.Flags[index] = flags
		}
		reason := ""
		switch {
		case file.Status == "renamed" && file.Similarity == 100 &&
			!file.ModeChanged && file.Additions == 0 && file.Deletions == 0 && len(file.Hunks) == 0:
			reason = "exact rename"
		case generated[index]:
			reason = "generated"
		case file.Binary:
			reason = "binary"
		}
		if reason != "" && !includeMechanical {
			result.Mechanical = append(result.Mechanical, index)
			result.MechanicalWhy[index] = reason
		} else {
			result.ReviewWorthy = append(result.ReviewWorthy, index)
		}
	}
	sortFileIndexes(files, result.ReviewWorthy)
	sortFileIndexes(files, result.Mechanical)
	return result
}

func heuristicFlags(file document.File) []string {
	normalized := strings.ToLower(filepath.ToSlash(file.Path))
	base := filepath.Base(normalized)
	var flags []string
	switch base {
	case "cargo.lock", "composer.lock", "gemfile.lock", "go.sum", "package-lock.json",
		"pnpm-lock.yaml", "poetry.lock", "uv.lock", "yarn.lock":
		flags = append(flags, "lockfile")
	}
	for _, component := range strings.Split(normalized, "/") {
		switch component {
		case "node_modules", "third_party", "vendor", "vendors":
			flags = append(flags, "vendored path")
		}
	}
	if strings.HasSuffix(normalized, ".pb.go") {
		flags = append(flags, "generated-looking filename")
	}
	if strings.Contains(base, ".min.") {
		flags = append(flags, "minified-looking filename")
	}
	if whitespaceOnlyChanges(file) {
		flags = append(flags, "whitespace-only changes")
	}
	return flags
}

func whitespaceOnlyChanges(file document.File) bool {
	changed := false
	var deleted, added strings.Builder
	for _, hunk := range file.Hunks {
		for _, line := range hunk.Lines {
			switch line.Type {
			case "a":
				changed = true
				added.WriteString(stripWhitespace(line.Text))
			case "d":
				changed = true
				deleted.WriteString(stripWhitespace(line.Text))
			}
		}
	}
	return changed && deleted.String() == added.String()
}

func stripWhitespace(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, value)
}

func PlanSummaryBatches(files []document.File, indexes []int, budget int) []SummaryBatch {
	return PlanSummaryBatchesWithSettings(files, indexes, budget, DefaultSettings())
}

func PlanSummaryBatchesWithSettings(files []document.File, indexes []int, budget int, settings Settings) []SummaryBatch {
	if budget <= 0 {
		budget = provider.DefaultAnalysisBudget
	}
	ordered := append([]int(nil), indexes...)
	sortFileIndexes(files, ordered)
	overhead := summaryBatchPromptOverheadChars()
	contentBudget := max(budget-overhead, 0)
	var result []SummaryBatch
	current := SummaryBatch{}
	for _, fileIndex := range ordered {
		headerChars := len(fileHeader(fileIndex, files[fileIndex]))
		fullDiffChars := len(fileDiffTextBounded(files[fileIndex], settings.FileDiffCap))
		inputChars := headerChars + fullDiffChars
		if len(current.Files) > 0 &&
			(len(current.Files) == settings.SummaryBatchMaxFiles ||
				current.InputChars+inputChars > contentBudget) {
			result = append(result, current)
			current = SummaryBatch{}
		}
		diffLimit := fullDiffChars
		remaining := max(contentBudget-current.InputChars-headerChars, 0)
		if diffLimit > remaining {
			diffLimit = remaining
		}
		current.Files = append(current.Files, fileIndex)
		current.DiffLimits = append(current.DiffLimits, diffLimit)
		current.InputChars += headerChars + len(fileDiffTextBounded(files[fileIndex], diffLimit))
	}
	if len(current.Files) > 0 {
		result = append(result, current)
	}
	for index := range result {
		result[index].InputChars += overhead
	}
	return result
}

func summaryInputChars(index int, file document.File) int {
	return len(fileHeader(index, file)) + len(fileDiffTextBounded(file, maxFileDiffCap))
}

func summaryBatchPromptOverheadChars() int {
	return len(BuildSummaryBatchPrompt(nil, SummaryBatch{}, Delimiters{
		Begin: "BEGIN_UNTRUSTED_0000000000000000",
		End:   "END_UNTRUSTED_0000000000000000",
	}))
}

func PlanCohorts(files []document.File) []PlannedCohort {
	return PlanCohortsWithMax(files, CohortMaxFiles)
}

func PlanCohortsWithMax(files []document.File, maxFiles int) []PlannedCohort {
	type groupKey struct {
		layer string
		top   string
	}
	groups := map[groupKey][]int{}
	for index, file := range files {
		parts := pathParts(file.Path)
		top := "."
		if len(parts) > 1 {
			top = parts[0]
		}
		key := groupKey{layer: pathlayer.Classify(file.Path), top: top}
		groups[key] = append(groups[key], index)
	}
	var result []PlannedCohort
	for key, indexes := range groups {
		sortFileIndexes(files, indexes)
		result = append(result, splitCohort(files, key.layer, key.top, indexes, 1, maxFiles)...)
	}
	sort.Slice(result, func(i, j int) bool {
		left := files[result[i].Files[0]].Path
		right := files[result[j].Files[0]].Path
		if left != right {
			return left < right
		}
		return result[i].Layer < result[j].Layer
	})
	return result
}

func splitCohort(files []document.File, layer, directory string, indexes []int, depth, maxFiles int) []PlannedCohort {
	if len(indexes) <= maxFiles {
		return []PlannedCohort{newPlannedCohort(layer, directory, indexes, 0, 0)}
	}
	hasSubdirectory := false
	for _, index := range indexes {
		if len(pathParts(files[index].Path)) > depth+1 {
			hasSubdirectory = true
			break
		}
	}
	if !hasSubdirectory {
		return chunkFlatCohort(layer, directory, indexes, maxFiles)
	}
	partitions := map[string][]int{}
	for _, index := range indexes {
		parts := pathParts(files[index].Path)
		component := ""
		if len(parts) > depth+1 {
			component = parts[depth]
		}
		partitions[component] = append(partitions[component], index)
	}
	keys := make([]string, 0, len(partitions))
	for key := range partitions {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var result []PlannedCohort
	for _, key := range keys {
		childDirectory := directory
		if key != "" {
			if childDirectory == "." {
				childDirectory = key
			} else {
				childDirectory += "/" + key
			}
		}
		child := partitions[key]
		if key == "" {
			result = append(result, chunkFlatCohort(layer, childDirectory, child, maxFiles)...)
		} else {
			result = append(result, splitCohort(files, layer, childDirectory, child, depth+1, maxFiles)...)
		}
	}
	return result
}

func chunkFlatCohort(layer, directory string, indexes []int, maxFiles int) []PlannedCohort {
	total := (len(indexes) + maxFiles - 1) / maxFiles
	result := make([]PlannedCohort, 0, total)
	for start, chunk := 0, 1; start < len(indexes); start, chunk = start+maxFiles, chunk+1 {
		end := min(start+maxFiles, len(indexes))
		result = append(result, newPlannedCohort(layer, directory, indexes[start:end], chunk, total))
	}
	return result
}

func newPlannedCohort(layer, directory string, indexes []int, chunk, total int) PlannedCohort {
	label := directory
	if label == "." {
		label = "Root"
	}
	title := fmt.Sprintf("%s %s changes", label, layer)
	if total > 1 {
		title += fmt.Sprintf(" (%d/%d)", chunk, total)
	}
	return PlannedCohort{
		Title: title, Layer: layer, Directory: directory, Files: append([]int(nil), indexes...),
	}
}

func BuildCohortDigest(
	files []document.File,
	cohort PlannedCohort,
	triage TriageResult,
	summaries []FileSummary,
	budget int,
) string {
	return BuildCohortDigestWithSettings(files, cohort, triage, summaries, budget, DefaultSettings())
}

func BuildCohortDigestWithSettings(
	files []document.File,
	cohort PlannedCohort,
	triage TriageResult,
	summaries []FileSummary,
	budget int,
	settings Settings,
) string {
	capChars := min(max(budget, 0), settings.DigestMaxChars)
	if capChars == 0 {
		return ""
	}
	mechanical := make(map[int]bool, len(triage.Mechanical))
	for _, index := range triage.Mechanical {
		mechanical[index] = true
	}
	additions, deletions, mechanicalCount, flaggedCount := 0, 0, 0, 0
	var candidates []int
	for _, index := range cohort.Files {
		file := files[index]
		additions += file.Additions
		deletions += file.Deletions
		if mechanical[index] {
			mechanicalCount++
			continue
		}
		candidates = append(candidates, index)
		if len(triage.Flags[index]) > 0 {
			flaggedCount++
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		left, right := candidates[i], candidates[j]
		leftFlagged := len(triage.Flags[left]) > 0
		rightFlagged := len(triage.Flags[right]) > 0
		if leftFlagged != rightFlagged {
			return leftFlagged
		}
		leftChurn := files[left].Additions + files[left].Deletions
		rightChurn := files[right].Additions + files[right].Deletions
		if leftChurn != rightChurn {
			return leftChurn > rightChurn
		}
		return files[left].Path < files[right].Path
	})

	var digest strings.Builder
	fmt.Fprintf(&digest,
		"COHORT: %s\nLAYER: %s\nDIRECTORY: %s\nFILES: %d\nREVIEW_WORTHY: %d\nMECHANICAL: %d\nFLAGGED: %d\nADDITIONS: %d\nDELETIONS: %d\n",
		cohort.Title, cohort.Layer, cohort.Directory, len(cohort.Files), len(candidates),
		mechanicalCount, flaggedCount, additions, deletions)
	digest.WriteString("SAMPLED_SUMMARIES:\n")
	for position, index := range candidates {
		if position >= settings.DigestMaxFiles {
			break
		}
		flags := strings.Join(triage.Flags[index], ", ")
		line := fmt.Sprintf("- FILE %d PATH=%s (+%d/-%d) FLAGS=%s SUMMARY=%s\n",
			index, jsonString(files[index].Path), files[index].Additions, files[index].Deletions,
			flags, jsonString(summaries[index].Summary))
		if digest.Len()+len(line) > capChars {
			break
		}
		digest.WriteString(line)
	}
	value := digest.String()
	if len(value) > capChars {
		return value[:capChars]
	}
	return value
}

func AssembleStagedAnalysis(
	files []document.File,
	plan StagedPlan,
	summaries []FileSummary,
	narrations []CohortNarration,
	synthesis Synthesis,
) document.Analysis {
	cohortFiles := make([][]int, len(plan.Cohorts))
	for index, cohort := range plan.Cohorts {
		cohortFiles[index] = cohort.Files
	}
	dependencies := make([][]int, len(plan.Cohorts))
	if plan.settings.CohortDependencies {
		dependencies = changegraph.CohortDependencies(cohortFiles, plan.edges)
	} else {
		for index := range dependencies {
			dependencies[index] = []int{}
		}
	}
	analysis := document.Analysis{
		Title:           synthesis.Title,
		Overview:        synthesis.Overview,
		Cohorts:         make([]document.Cohort, len(plan.Cohorts)),
		StubbedFiles:    []int{},
		MechanicalFiles: append([]int{}, plan.Triage.Mechanical...),
		FileKeySymbols:  make([][]string, len(files)),
		StubbedCohorts:  []int{},
	}
	for index := range analysis.FileKeySymbols {
		analysis.FileKeySymbols[index] = append([]string{}, summaries[index].KeySymbols...)
	}
	for cohortIndex, planned := range plan.Cohorts {
		narration := narrations[cohortIndex]
		cohort := document.Cohort{
			Title: narration.Title, Layer: planned.Layer, Intent: narration.Intent,
			Narrative: narration.Narrative, Files: append([]int(nil), planned.Files...),
			FileSummaries: make([]string, len(planned.Files)),
			ReviewNotes:   append([]string{}, narration.ReviewNotes...),
			DependsOn:     dependencies[cohortIndex],
		}
		for position, fileIndex := range planned.Files {
			cohort.FileSummaries[position] = summaries[fileIndex].Summary
		}
		analysis.Cohorts[cohortIndex] = cohort
	}
	for index, summary := range summaries {
		if summary.Stubbed {
			analysis.StubbedFiles = append(analysis.StubbedFiles, index)
		}
	}
	sortFileIndexes(files, analysis.StubbedFiles)
	return analysis
}

func sortFileIndexes(files []document.File, indexes []int) {
	sort.Slice(indexes, func(i, j int) bool {
		left, right := files[indexes[i]].Path, files[indexes[j]].Path
		if left != right {
			return left < right
		}
		return indexes[i] < indexes[j]
	})
}

func pathParts(path string) []string {
	normalized := strings.Trim(filepath.ToSlash(path), "/")
	if normalized == "" {
		return []string{""}
	}
	return strings.Split(normalized, "/")
}
