package document

const (
	SchemaVersion = 4
)

var Version = "dev"

var Layers = []string{"schema", "backend", "api", "ui", "tests", "config", "docs", "other"}

type Document struct {
	SchemaVersion int      `json:"schemaVersion"`
	Source        Source   `json:"source"`
	Files         []File   `json:"files"`
	Analysis      Analysis `json:"analysis"`
	Meta          Meta     `json:"meta"`
}

type Source struct {
	Title       string  `json:"title"`
	Description string  `json:"description"`
	Range       string  `json:"range"`
	URL         *string `json:"url"`
	Name        string  `json:"name"`
	RepoDir     string  `json:"repoDir"`
}

type File struct {
	Path      string `json:"path"`
	OldPath   string `json:"oldPath"`
	NewPath   string `json:"newPath"`
	Status    string `json:"status"`
	Binary    bool   `json:"binary"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Hunks     []Hunk `json:"hunks"`
}

type Hunk struct {
	Header string     `json:"header"`
	Lines  []HunkLine `json:"lines"`
	Blame  *Blame     `json:"blame,omitempty"`
}

type Blame struct {
	Author string `json:"author"`
	Date   string `json:"date"`
}

type HunkLine struct {
	Type string `json:"t"`
	Old  int    `json:"old"`
	New  int    `json:"new"`
	Text string `json:"text"`
}

type Analysis struct {
	Title        string   `json:"title"`
	Overview     string   `json:"overview"`
	Cohorts      []Cohort `json:"cohorts"`
	StubbedFiles []int    `json:"stubbedFiles"`
}

type Cohort struct {
	Title         string   `json:"title"`
	Layer         string   `json:"layer"`
	Intent        string   `json:"intent"`
	Narrative     string   `json:"narrative"`
	Files         []int    `json:"files"`
	FileSummaries []string `json:"fileSummaries"`
	ReviewNotes   []string `json:"reviewNotes"`
	DependsOn     []int    `json:"dependsOn"`
}

type Meta struct {
	Provider  string `json:"provider"`
	Model     string `json:"model"`
	Generator string `json:"generator"`
	Cached    bool   `json:"cached"`
	Staged    bool   `json:"staged"`
}

func Generator() string {
	return "bgr " + Version
}

func IsLayer(layer string) bool {
	for _, candidate := range Layers {
		if layer == candidate {
			return true
		}
	}
	return false
}
