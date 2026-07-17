package pathlayer

import (
	"path/filepath"
	"regexp"
	"strings"
)

type rule struct {
	layer   string
	pattern *regexp.Regexp
}

var rules = []rule{
	{layer: "schema", pattern: regexp.MustCompile(`migration|schema|\.sql$|models?/`)},
	{layer: "tests", pattern: regexp.MustCompile(`test|spec|__tests__|\.test\.|\.spec\.`)},
	{layer: "api", pattern: regexp.MustCompile(`routes?|api|controller|endpoint|graphql|resolver`)},
	{layer: "ui", pattern: regexp.MustCompile(`component|page|view|\.css|\.scss|\.html$|frontend|ui/|\.tsx$|\.jsx$|\.vue$`)},
	{layer: "config", pattern: regexp.MustCompile(`\.(json|ya?ml|toml|ini|env|cfg)$|dockerfile|makefile|\.github/`)},
	{layer: "docs", pattern: regexp.MustCompile(`\.(md|rst|txt)$|docs?/`)},
}

func Classify(path string) string {
	normalized := strings.ToLower(filepath.ToSlash(path))
	for _, candidate := range rules {
		if candidate.pattern.MatchString(normalized) {
			return candidate.layer
		}
	}
	return "backend"
}
