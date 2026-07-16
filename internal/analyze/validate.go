package analyze

import (
	"fmt"
	"strings"

	"github.com/janiorvalle/better-git-review/internal/document"
)

func ApplySeatbelts(analysis document.Analysis, fileCount int) document.Analysis {
	seen := make(map[int]bool, fileCount)
	type retainedCohort struct {
		cohort document.Cohort
	}
	retained := make([]retainedCohort, 0, len(analysis.Cohorts))
	originalToNormalized := make(map[int]int, len(analysis.Cohorts))

	for originalIndex, cohort := range analysis.Cohorts {
		if !document.IsLayer(cohort.Layer) {
			cohort.Layer = "other"
		}
		if cohort.ReviewNotes == nil {
			cohort.ReviewNotes = []string{}
		}

		var files []int
		var summaries []string
		for position, fileIndex := range cohort.Files {
			if fileIndex < 0 || fileIndex >= fileCount || seen[fileIndex] {
				continue
			}
			seen[fileIndex] = true
			files = append(files, fileIndex)
			summary := ""
			if position < len(cohort.FileSummaries) {
				summary = cohort.FileSummaries[position]
			}
			summaries = append(summaries, summary)
		}
		if len(files) == 0 {
			continue
		}
		cohort.Files = files
		cohort.FileSummaries = summaries
		originalToNormalized[originalIndex] = len(retained)
		retained = append(retained, retainedCohort{cohort: cohort})
	}

	normalized := make([]document.Cohort, 0, len(retained)+1)
	for normalizedIndex, item := range retained {
		item.cohort.DependsOn = remapDependencies(
			item.cohort.DependsOn, originalToNormalized, normalizedIndex,
		)
		normalized = append(normalized, item.cohort)
	}

	var leftovers []int
	for index := 0; index < fileCount; index++ {
		if !seen[index] {
			leftovers = append(leftovers, index)
		}
	}
	if len(leftovers) > 0 {
		summaries := make([]string, len(leftovers))
		for index := range summaries {
			summaries[index] = "No model summary was available for this file."
		}
		normalized = append(normalized, document.Cohort{
			Title:         "Other changes",
			Layer:         "other",
			Intent:        "Files the analysis did not assign to a cohort.",
			Narrative:     "Review these unassigned files after the model-defined cohorts.",
			Files:         leftovers,
			FileSummaries: summaries,
			ReviewNotes:   []string{},
			DependsOn:     []int{},
		})
	}
	analysis.Cohorts = normalized
	return analysis
}

func Validate(analysis document.Analysis, fileCount int) []string {
	var errors []string
	if len(analysis.Cohorts) == 0 {
		errors = append(errors, "cohorts must contain at least one item")
		return errors
	}
	seen := make(map[int]int, fileCount)
	for cohortIndex, cohort := range analysis.Cohorts {
		prefix := fmt.Sprintf("cohorts[%d]", cohortIndex)
		if !document.IsLayer(cohort.Layer) {
			errors = append(errors, prefix+".layer is not in the allowed enum")
		}
		if len(cohort.Files) == 0 {
			errors = append(errors, prefix+".files must not be empty")
		}
		if len(cohort.FileSummaries) != len(cohort.Files) {
			errors = append(errors, prefix+".fileSummaries must be parallel to files")
		}
		for _, fileIndex := range cohort.Files {
			if fileIndex < 0 || fileIndex >= fileCount {
				errors = append(errors, fmt.Sprintf("%s.files contains out-of-range index %d", prefix, fileIndex))
				continue
			}
			seen[fileIndex]++
		}
		for _, dependency := range cohort.DependsOn {
			if dependency < 0 || dependency >= cohortIndex {
				errors = append(errors,
					fmt.Sprintf("%s.dependsOn contains %d, which is not an earlier cohort", prefix, dependency))
			}
		}
	}
	for fileIndex := 0; fileIndex < fileCount; fileIndex++ {
		switch seen[fileIndex] {
		case 0:
			errors = append(errors, fmt.Sprintf("file index %d is not assigned to a cohort", fileIndex))
		case 1:
		default:
			errors = append(errors, fmt.Sprintf("file index %d is assigned to multiple cohorts", fileIndex))
		}
	}
	return errors
}

func ValidateComplete(analysis document.Analysis, fileCount int) []string {
	errors := validateRequiredContent(analysis)
	return append(errors, Validate(analysis, fileCount)...)
}

func validateBeforeSeatbelts(analysis document.Analysis, fileCount int) []string {
	errors := validateRequiredContent(analysis)
	if len(analysis.Cohorts) == 0 {
		return errors
	}
	for cohortIndex, cohort := range analysis.Cohorts {
		prefix := fmt.Sprintf("cohorts[%d]", cohortIndex)
		if len(cohort.FileSummaries) != len(cohort.Files) {
			errors = append(errors, prefix+".fileSummaries must be parallel to files")
		}
		for _, fileIndex := range cohort.Files {
			if fileIndex < 0 || fileIndex >= fileCount {
				errors = append(errors, fmt.Sprintf("%s.files contains out-of-range index %d", prefix, fileIndex))
			}
		}
	}
	return errors
}

func validateRequiredContent(analysis document.Analysis) []string {
	var errors []string
	if strings.TrimSpace(analysis.Title) == "" {
		errors = append(errors, "title must not be empty")
	}
	if strings.TrimSpace(analysis.Overview) == "" {
		errors = append(errors, "overview must not be empty")
	}
	if len(analysis.Cohorts) == 0 {
		errors = append(errors, "cohorts must contain at least one item")
		return errors
	}
	for cohortIndex, cohort := range analysis.Cohorts {
		prefix := fmt.Sprintf("cohorts[%d]", cohortIndex)
		if strings.TrimSpace(cohort.Title) == "" {
			errors = append(errors, prefix+".title must not be empty")
		}
		if strings.TrimSpace(cohort.Layer) == "" {
			errors = append(errors, prefix+".layer must not be empty")
		}
		if strings.TrimSpace(cohort.Intent) == "" {
			errors = append(errors, prefix+".intent must not be empty")
		}
		if strings.TrimSpace(cohort.Narrative) == "" {
			errors = append(errors, prefix+".narrative must not be empty")
		}
		if cohort.Files == nil {
			errors = append(errors, prefix+".files must be present")
		}
		if cohort.FileSummaries == nil {
			errors = append(errors, prefix+".fileSummaries must be present")
		}
		if cohort.ReviewNotes == nil {
			errors = append(errors, prefix+".reviewNotes must be present")
		}
		if cohort.DependsOn == nil {
			errors = append(errors, prefix+".dependsOn must be present")
		}
	}
	return errors
}

func FormatErrors(errors []string) string {
	return strings.Join(errors, "; ")
}

func remapDependencies(dependencies []int, indexMap map[int]int, currentIndex int) []int {
	seen := map[int]bool{}
	result := make([]int, 0, len(dependencies))
	for _, dependency := range dependencies {
		remapped, ok := indexMap[dependency]
		if !ok || remapped < 0 || remapped >= currentIndex || seen[remapped] {
			continue
		}
		seen[remapped] = true
		result = append(result, remapped)
	}
	return result
}
