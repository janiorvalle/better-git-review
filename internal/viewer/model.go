package viewer

import (
	"encoding/json"
	"fmt"
	"html/template"
	"strings"

	"github.com/janiorvalle/better-git-review/internal/document"
)

type Page struct {
	Title        string
	Range        string
	JSON         template.JS
	ChromaTokens template.CSS
	ChromaLight  template.CSS
	ChromaDark   template.CSS
	TotalFiles   int
	Additions    int
	Deletions    int
	Mermaid      *string
	Overview     string
	Steps        []StepView
	Files        []FileView
}

type StepView struct {
	Index        int
	Number       int
	ID           string
	Title        string
	Layer        string
	Intent       string
	Narrative    string
	ReviewNotes  []string
	Dependencies []DependencyView
	FileIndexes  []int
	FileCount    int
	IsOverview   bool
}

type DependencyView struct {
	Title     string
	StepIndex int
}

type FileView struct {
	Index       int
	Path        string
	Status      string
	Additions   int
	Deletions   int
	Binary      bool
	Summary     string
	Stubbed     bool
	Collapsed   bool
	UnifiedRows []UnifiedRow
	SplitRows   []SplitRow
}

func buildPage(doc document.Document) (Page, error) {
	encoded, err := json.Marshal(doc)
	if err != nil {
		return Page{}, err
	}
	jsonIsland := strings.ReplaceAll(string(encoded), "<", `\u003c`)
	chromaTheme, err := ChromaThemeCSS("github", "github-dark")
	if err != nil {
		return Page{}, err
	}
	page := Page{
		Title:        firstNonEmpty(doc.Analysis.Title, doc.Source.Title),
		Range:        doc.Source.Range,
		JSON:         template.JS(jsonIsland),
		ChromaTokens: chromaTheme.TokenCSS,
		ChromaLight:  chromaTheme.LightVariables,
		ChromaDark:   chromaTheme.DarkVariables,
		TotalFiles:   len(doc.Files),
		Mermaid:      doc.Analysis.Mermaid,
		Overview:     doc.Analysis.Overview,
		Files:        make([]FileView, len(doc.Files)),
	}
	stubbedFiles := make(map[int]bool, len(doc.Analysis.StubbedFiles))
	for _, fileIndex := range doc.Analysis.StubbedFiles {
		stubbedFiles[fileIndex] = true
	}
	for index, file := range doc.Files {
		unified, split := BuildRows(file, index)
		page.Additions += file.Additions
		page.Deletions += file.Deletions
		page.Files[index] = FileView{
			Index:       index,
			Path:        file.Path,
			Status:      file.Status,
			Additions:   file.Additions,
			Deletions:   file.Deletions,
			Binary:      file.Binary,
			Stubbed:     stubbedFiles[index],
			Collapsed:   file.Additions+file.Deletions > 400,
			UnifiedRows: unified,
			SplitRows:   split,
		}
	}
	page.Steps = append(page.Steps, StepView{
		Index: 0, ID: "step-overview", Title: "Overview", IsOverview: true,
		FileCount: len(doc.Files),
	})
	for cohortIndex, cohort := range doc.Analysis.Cohorts {
		step := StepView{
			Index:       cohortIndex + 1,
			Number:      cohortIndex + 1,
			ID:          fmt.Sprintf("step-%d", cohortIndex+1),
			Title:       cohort.Title,
			Layer:       cohort.Layer,
			Intent:      cohort.Intent,
			Narrative:   cohort.Narrative,
			ReviewNotes: cohort.ReviewNotes,
			FileIndexes: cohort.Files,
			FileCount:   len(cohort.Files),
		}
		for _, dependency := range cohort.DependsOn {
			if dependency >= 0 && dependency < len(doc.Analysis.Cohorts) {
				step.Dependencies = append(step.Dependencies, DependencyView{
					Title:     doc.Analysis.Cohorts[dependency].Title,
					StepIndex: dependency + 1,
				})
			}
		}
		for summaryIndex, fileIndex := range cohort.Files {
			if fileIndex >= 0 && fileIndex < len(page.Files) && summaryIndex < len(cohort.FileSummaries) {
				page.Files[fileIndex].Summary = cohort.FileSummaries[summaryIndex]
			}
		}
		page.Steps = append(page.Steps, step)
	}
	return page, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return "bgr"
}
