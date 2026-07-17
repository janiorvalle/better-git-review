package viewer

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"path/filepath"
	"strings"

	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/janiorvalle/better-git-review/internal/document"
	"github.com/janiorvalle/better-git-review/internal/media"
)

type Page struct {
	Title            string
	Range            string
	JSON             template.JS
	ChromaTokens     template.CSS
	ChromaLight      template.CSS
	ChromaDark       template.CSS
	ChromaLightRules template.CSS
	ChromaDarkRules  template.CSS
	TotalFiles       int
	Additions        int
	Deletions        int
	CohortCount      int
	MechanicalCount  int
	MechanicalFiles  []MechanicalFileView
	DocID            string
	Diagram          template.HTML
	Overview         string
	Steps            []StepView
	Files            []FileView
}

type MechanicalFileView struct {
	Path   string
	Status string
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
	FileList     string
	FileCount    int
	IsOverview   bool
}

type DependencyView struct {
	Title     string
	StepIndex int
}

type FileView struct {
	Index        int
	Path         string
	Status       string
	Lang         string
	Additions    int
	Deletions    int
	Binary       bool
	Summary      string
	Stubbed      bool
	Mechanical   bool
	Collapsed    bool
	StepPosition int
	StepTotal    int
	PrevFile     int
	NextFile     int
	UnifiedRows  []UnifiedRow
	SplitRows    []SplitRow
	BinaryLabel  string
	ImagePreview bool
	OldImage     *ImageAssetView
	NewImage     *ImageAssetView
}

type ImageAssetView struct {
	DataURI    template.URL
	SizeLabel  string
	Dimensions string
}

func buildPage(doc document.Document) (Page, error) {
	return buildPageWithPreviews(doc, nil)
}

func buildPageWithPreviews(doc document.Document, previews map[int]media.Preview) (Page, error) {
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
		Title:            firstNonEmpty(doc.Analysis.Title, doc.Source.Title),
		Range:            doc.Source.Range,
		JSON:             template.JS(jsonIsland),
		ChromaTokens:     chromaTheme.TokenCSS,
		ChromaLight:      chromaTheme.LightVariables,
		ChromaDark:       chromaTheme.DarkVariables,
		ChromaLightRules: chromaTheme.LightRules,
		ChromaDarkRules:  chromaTheme.DarkRules,
		TotalFiles:       len(doc.Files),
		CohortCount:      len(doc.Analysis.Cohorts),
		DocID:            docID(doc),
		Overview:         doc.Analysis.Overview,
		Files:            make([]FileView, len(doc.Files)),
	}
	stubbedFiles := make(map[int]bool, len(doc.Analysis.StubbedFiles))
	for _, fileIndex := range doc.Analysis.StubbedFiles {
		stubbedFiles[fileIndex] = true
	}
	mechanicalFiles := make(map[int]bool, len(doc.Analysis.MechanicalFiles))
	for _, fileIndex := range doc.Analysis.MechanicalFiles {
		mechanicalFiles[fileIndex] = true
	}
	page.MechanicalCount = len(mechanicalFiles)
	for index, file := range doc.Files {
		unified, split := BuildRows(file, index)
		page.Additions += file.Additions
		page.Deletions += file.Deletions
		preview := previews[index]
		binaryLabel := preview.Label
		if binaryLabel == "" && file.Binary {
			binaryLabel = "Binary file"
		}
		page.Files[index] = FileView{
			Index:        index,
			Path:         file.Path,
			Status:       file.Status,
			Lang:         langChip(file.Path, file.Binary),
			Additions:    file.Additions,
			Deletions:    file.Deletions,
			Binary:       file.Binary,
			Stubbed:      stubbedFiles[index],
			Mechanical:   mechanicalFiles[index],
			Collapsed:    file.Additions+file.Deletions > 400,
			PrevFile:     -1,
			NextFile:     -1,
			UnifiedRows:  unified,
			SplitRows:    split,
			BinaryLabel:  binaryLabel,
			ImagePreview: preview.Old != nil || preview.New != nil,
			OldImage:     imageAssetView(preview.Old),
			NewImage:     imageAssetView(preview.New),
		}
		if mechanicalFiles[index] {
			page.MechanicalFiles = append(page.MechanicalFiles, MechanicalFileView{
				Path: file.Path, Status: file.Status,
			})
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
		valid := make([]int, 0, len(cohort.Files))
		for _, fileIndex := range cohort.Files {
			if fileIndex >= 0 && fileIndex < len(page.Files) {
				valid = append(valid, fileIndex)
			}
		}
		for position, fileIndex := range valid {
			if position < len(cohort.FileSummaries) {
				page.Files[fileIndex].Summary = cohort.FileSummaries[position]
			}
			page.Files[fileIndex].StepPosition = position + 1
			page.Files[fileIndex].StepTotal = len(valid)
			if position > 0 {
				page.Files[fileIndex].PrevFile = valid[position-1]
			}
			if position < len(valid)-1 {
				page.Files[fileIndex].NextFile = valid[position+1]
			}
		}
		step.FileList = joinIndexes(valid)
		page.Steps = append(page.Steps, step)
	}
	page.Diagram = BuildDiagram(page.Steps)
	return page, nil
}

func imageAssetView(asset *media.Asset) *ImageAssetView {
	if asset == nil || !strings.HasPrefix(asset.DataURI, "data:image/") {
		return nil
	}
	return &ImageAssetView{
		DataURI: template.URL(asset.DataURI), SizeLabel: asset.SizeLabel, Dimensions: asset.Dimensions,
	}
}

func docID(doc document.Document) string {
	hash := sha256.New()
	fmt.Fprintf(hash, "%s\x00%d\x00", doc.Source.Range, doc.SchemaVersion)
	for _, file := range doc.Files {
		fmt.Fprintf(hash, "%s\x00%d\x00%d\x00", file.Path, file.Additions, file.Deletions)
	}
	return hex.EncodeToString(hash.Sum(nil))[:16]
}

func langChip(path string, binary bool) string {
	if binary {
		return "BIN"
	}
	// Extensions that are more specific than chroma's lexer name (chroma
	// files .tsx under "TypeScript", losing information a reviewer wants).
	ext := strings.TrimPrefix(filepath.Ext(path), ".")
	switch strings.ToLower(ext) {
	case "tsx", "jsx", "vue", "svelte":
		return strings.ToUpper(ext)
	}
	// Otherwise ask chroma — it knows well-known filenames (Dockerfile,
	// Makefile) that extension sniffing misses, and it is the same
	// authority that picks the highlighting lexer.
	if lexer := lexers.Match(filepath.Base(path)); lexer != nil {
		name := lexer.Config().Name
		if alias := chipAliases[name]; alias != "" {
			return alias
		}
		if len(name) > 6 {
			name = name[:6]
		}
		return strings.ToUpper(name)
	}
	if ext == "" {
		return "TXT"
	}
	if len(ext) > 5 {
		ext = ext[:5]
	}
	return strings.ToUpper(ext)
}

// chipAliases shortens chroma lexer names that would make clumsy chips.
var chipAliases = map[string]string{
	"Docker":        "DOCKER",
	"Makefile":      "MAKE",
	"TypeScript":    "TS",
	"JavaScript":    "JS",
	"Plaintext":     "TXT",
	"markdown":      "MD",
	"Base Makefile": "MAKE",
	"TSX":           "TSX",
}

func joinIndexes(indexes []int) string {
	parts := make([]string, len(indexes))
	for position, value := range indexes {
		parts[position] = fmt.Sprintf("%d", value)
	}
	return strings.Join(parts, ",")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return "bgr"
}
