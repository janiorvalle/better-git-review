package viewer

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/janiorvalle/better-git-review/internal/document"
	"github.com/janiorvalle/better-git-review/internal/media"
)

const FidelityBudgetChars = 4_000_000

type Page struct {
	Title            string
	Range            string
	JSON             template.JS
	DiffJSON         template.JS
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
	ClientFileCards  bool
	DocID            string
	Diagram          template.HTML
	Overview         string
	Steps            []StepView
	Files            []FileView
	FidelityFiles    []FidelityFileView
}

type FidelityFileView struct {
	Index int
	HTML  template.HTML
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
	Stubbed      bool
}

type DependencyView struct {
	Title     string
	StepIndex int
}

type FileView struct {
	Index            int
	Path             string
	Status           string
	Lang             string
	Additions        int
	Deletions        int
	Binary           bool
	Summary          string
	Stubbed          bool
	Mechanical       bool
	KeySymbols       []string
	Collapsed        bool
	ClientRows       bool
	StepPosition     int
	StepTotal        int
	PrevFile         int
	NextFile         int
	UnifiedRows      []UnifiedRow
	FullFidelityHTML string
	BinaryLabel      string
	ImagePreview     bool
	OldImage         *ImageAssetView
	NewImage         *ImageAssetView
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
	metadata := doc
	metadata.Files = append([]document.File(nil), doc.Files...)
	for index := range metadata.Files {
		metadata.Files[index].Hunks = nil
	}
	encoded, err := json.Marshal(metadata)
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
		ClientFileCards:  doc.Meta.Staged,
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
	fullFidelity := map[int]string{}
	if doc.Meta.Staged {
		fidelityBudget := min(FidelityBudgetChars, stagedFidelityCeilingBudget(doc.Files, len(encoded)))
		fullFidelity, err = planFullFidelity(doc.Files, mechanicalFiles, fidelityBudget)
		if err != nil {
			return Page{}, err
		}
	}
	for index, file := range doc.Files {
		var unified []UnifiedRow
		clientRows := doc.Meta.Staged && !file.Binary && fullFidelity[index] == ""
		if !doc.Meta.Staged && !clientRows {
			unified = BuildRows(file, index)
		}
		page.Additions += file.Additions
		page.Deletions += file.Deletions
		preview := previews[index]
		binaryLabel := preview.Label
		if binaryLabel == "" && file.Binary {
			binaryLabel = "Binary file"
		}
		page.Files[index] = FileView{
			Index:            index,
			Path:             file.Path,
			Status:           file.Status,
			Lang:             langChip(file.Path, file.Binary),
			Additions:        file.Additions,
			Deletions:        file.Deletions,
			Binary:           file.Binary,
			Stubbed:          stubbedFiles[index],
			Mechanical:       mechanicalFiles[index],
			KeySymbols:       cappedSymbols(doc.Analysis.FileKeySymbols, index),
			Collapsed:        file.Additions+file.Deletions > 400,
			ClientRows:       clientRows,
			PrevFile:         -1,
			NextFile:         -1,
			UnifiedRows:      unified,
			FullFidelityHTML: fullFidelity[index],
			BinaryLabel:      binaryLabel,
			ImagePreview:     preview.Old != nil || preview.New != nil,
			OldImage:         imageAssetView(preview.Old),
			NewImage:         imageAssetView(preview.New),
		}
		if fullFidelity[index] != "" {
			page.FidelityFiles = append(page.FidelityFiles, FidelityFileView{
				Index: index, HTML: template.HTML(fullFidelity[index]),
			})
		}
		if mechanicalFiles[index] {
			page.MechanicalFiles = append(page.MechanicalFiles, MechanicalFileView{
				Path: file.Path, Status: file.Status,
			})
		}
	}
	diffIsland := `{"f":[],"u":[]}`
	if doc.Meta.Staged {
		diffIsland, err = compactDiffJSON(doc.Files, page.Files)
		if err != nil {
			return Page{}, err
		}
	}
	page.DiffJSON = template.JS(diffIsland)
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
			Stubbed:     containsIndex(doc.Analysis.StubbedCohorts, cohortIndex),
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

func stagedFidelityCeilingBudget(files []document.File, metadataChars int) int {
	diffChars := 0
	for _, file := range files {
		for _, hunk := range file.Hunks {
			diffChars += len(hunk.Header)
			for _, line := range hunk.Lines {
				diffChars += len(line.Text) + 1
			}
		}
	}
	const viewerShellReserve = 200_000
	ceiling := 2*1_024*1_024 + diffChars*5/2
	// Conservatively reserve the metadata, shell, and worst-case compact
	// encoding. Selected files are removed from the compact payload later,
	// so this can only leave extra headroom.
	return max(ceiling-metadataChars-viewerShellReserve-diffChars*2, 0)
}

func cappedSymbols(all [][]string, index int) []string {
	if index < 0 || index >= len(all) {
		return nil
	}
	return append([]string(nil), all[index][:min(len(all[index]), 5)]...)
}

func containsIndex(indexes []int, target int) bool {
	for _, index := range indexes {
		if index == target {
			return true
		}
	}
	return false
}

type fidelityCandidate struct {
	index int
	path  string
	size  int
}

func planFullFidelity(files []document.File, mechanical map[int]bool, budget int) (map[int]string, error) {
	if budget <= 0 {
		return map[int]string{}, nil
	}
	var indexes []int
	for index, file := range files {
		if file.Binary || mechanical[index] {
			continue
		}
		// Escaped code text alone is a lower bound on full row HTML. A file
		// above the entire budget cannot be selected regardless of markup,
		// highlighting, or its position in rendered-size order.
		if fidelityTextLowerBound(file) > budget {
			continue
		}
		indexes = append(indexes, index)
	}
	candidates := make([]fidelityCandidate, len(indexes))
	jobs := make(chan int)
	var workers sync.WaitGroup
	var firstErr error
	var errOnce sync.Once
	// Selection is defined by actual full-fidelity HTML size, not raw diff
	// size. Measure one file per worker and discard the HTML so monster
	// artifacts never retain every highlighted candidate at once. The chosen
	// subset is rendered again below; the bounded CPU tradeoff keeps selection
	// exact and the stress suite enforces its wall-time and RSS budgets.
	for range min(8, len(indexes)) {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for position := range jobs {
				index := indexes[position]
				file := files[index]
				var output bytes.Buffer
				if err := unifiedRowsTemplate.Execute(&output, struct {
					Path string
					Rows []UnifiedRow
				}{file.Path, BuildRows(file, index)}); err != nil {
					errOnce.Do(func() { firstErr = err })
					continue
				}
				candidates[position] = fidelityCandidate{index: index, path: file.Path, size: output.Len()}
			}
		}()
	}
	for position := range indexes {
		jobs <- position
	}
	close(jobs)
	workers.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].size != candidates[j].size {
			return candidates[i].size < candidates[j].size
		}
		return candidates[i].path < candidates[j].path
	})
	var chosen []fidelityCandidate
	spent := 0
	for _, candidate := range candidates {
		if spent+candidate.size > budget {
			break
		}
		chosen = append(chosen, candidate)
		spent += candidate.size
	}
	rendered := make([]string, len(chosen))
	jobs = make(chan int)
	workers = sync.WaitGroup{}
	firstErr = nil
	errOnce = sync.Once{}
	for range min(8, len(chosen)) {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for position := range jobs {
				candidate := chosen[position]
				file := files[candidate.index]
				var output bytes.Buffer
				if err := unifiedRowsTemplate.Execute(&output, struct {
					Path string
					Rows []UnifiedRow
				}{file.Path, BuildRows(file, candidate.index)}); err != nil {
					errOnce.Do(func() { firstErr = err })
					continue
				}
				rendered[position] = output.String()
			}
		}()
	}
	for position := range chosen {
		jobs <- position
	}
	close(jobs)
	workers.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	selected := make(map[int]string, len(chosen))
	for position, candidate := range chosen {
		selected[candidate.index] = rendered[position]
	}
	return selected, nil
}

func fidelityTextLowerBound(file document.File) int {
	total := 0
	for _, hunk := range file.Hunks {
		total += len(template.HTMLEscapeString(hunkLabel(hunk)))
		for _, line := range hunk.Lines {
			total += len(template.HTMLEscapeString(line.Text))
		}
	}
	return total
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
