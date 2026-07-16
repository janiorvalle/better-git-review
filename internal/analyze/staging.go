package analyze

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/janiorvalle/better-git-review/internal/document"
)

type StageDecision struct {
	Staged     bool
	InputBytes int
	Budget     int
}

func DecideStaging(files []document.File, getenv func(string) string) (StageDecision, error) {
	budget, err := StageBudget(getenv)
	if err != nil {
		return StageDecision{}, err
	}
	inputBytes := AnalysisInputBytes(files)
	return StageDecision{
		Staged:     inputBytes > budget,
		InputBytes: inputBytes,
		Budget:     budget,
	}, nil
}

func pathLayerHint(path string) string {
	lower := strings.ToLower(filepath.ToSlash(path))
	switch {
	case regexp.MustCompile(`migration|schema|\.sql$|models?/`).MatchString(lower):
		return "schema"
	case regexp.MustCompile(`test|spec|__tests__|\.test\.|\.spec\.`).MatchString(lower):
		return "tests"
	case regexp.MustCompile(`routes?|api|controller|endpoint|graphql|resolver`).MatchString(lower):
		return "api"
	case regexp.MustCompile(`component|page|view|\.css|\.scss|\.html$|frontend|ui/|\.tsx$|\.jsx$|\.vue$`).MatchString(lower):
		return "ui"
	case regexp.MustCompile(`\.(json|ya?ml|toml|ini|env|cfg)$|dockerfile|makefile|\.github/`).MatchString(lower):
		return "config"
	case regexp.MustCompile(`\.(md|rst|txt)$|docs?/`).MatchString(lower):
		return "docs"
	default:
		return "backend"
	}
}
