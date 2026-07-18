package changegraph

import (
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/janiorvalle/better-git-review/internal/document"
)

type Edge struct {
	Importer int
	Imported int
}

var (
	jsFromPattern       = regexp.MustCompile(`\bfrom\s*["']([^"']+)["']`)
	jsFromKeyword       = regexp.MustCompile(`\bfrom\b`)
	jsFromLineStart     = regexp.MustCompile(`^\s*from\b`)
	jsQuotedSpecifier   = regexp.MustCompile(`^\s*["']([^"']+)["']`)
	jsImportStart       = regexp.MustCompile(`^\s*import\b`)
	jsSideEffectImport  = regexp.MustCompile(`^\s*import\s*["']`)
	jsExportFromStart   = regexp.MustCompile(`^\s*export\s+(?:type\s+)?(?:\{|\*)`)
	jsRequirePattern    = regexp.MustCompile(`(?:^\s*|[=(:,]\s*|\breturn\s+)require\s*\(\s*["']([^"']+)["']\s*\)`)
	goImportPattern     = regexp.MustCompile(`^\s*import\s+(?:[._A-Za-z][A-Za-z0-9_]*\s+)?["\x60]([^"\x60]+)["\x60]`)
	goImportSpecPattern = regexp.MustCompile(`^\s*(?:[._A-Za-z][A-Za-z0-9_]*\s+)?["\x60]([^"\x60]+)["\x60]\s*(?://.*)?$`)
	pythonFromPattern   = regexp.MustCompile(`^\s*from\s+([.A-Za-z_][.A-Za-z0-9_]*)\s+import\s+`)
	pythonImportPattern = regexp.MustCompile(`^\s*import\s+([^#]+)`)
	pythonModulePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.]*$`)
	javaImportPattern   = regexp.MustCompile(`^\s*import\s+(?:(static)\s+)?([A-Za-z_$][A-Za-z0-9_$]*(?:\.(?:[A-Za-z_$][A-Za-z0-9_$]*|\*))*)\s*;\s*(?://.*)?$`)
	kotlinImportPattern = regexp.MustCompile(`^\s*import\s+([A-Za-z_$][A-Za-z0-9_$]*(?:\.(?:[A-Za-z_$][A-Za-z0-9_$]*|\*))*)\s*(?:as\s+[A-Za-z_$][A-Za-z0-9_$]*)?\s*;?\s*(?://.*)?$`)

	jsDefinitionPattern             = regexp.MustCompile(`^\s*export\s+(?:async\s+)?(?:function|const|class|type|interface)\s+([A-Za-z_$][A-Za-z0-9_$]*)`)
	goDefinitionPattern             = regexp.MustCompile(`^\s*(?:func|type)\s+([A-Z][A-Za-z0-9_]*)\b`)
	pythonDefinitionPattern         = regexp.MustCompile(`^(?:async\s+)?(?:def|class)\s+([A-Za-z_][A-Za-z0-9_]*)\b`)
	javaDefinitionPattern           = regexp.MustCompile(`^\s*(?:public\s+)?(?:(?:final|abstract|sealed)\s+)*(?:class|interface|enum|record)\s+([A-Za-z_$][A-Za-z0-9_$]*)(?:[^A-Za-z0-9_$]|$)`)
	kotlinTypeDefinitionPattern     = regexp.MustCompile(`^(?:class|interface|object)\s+([A-Za-z_$][A-Za-z0-9_$]*)(?:[^A-Za-z0-9_$]|$)`)
	kotlinFunctionDefinitionPattern = regexp.MustCompile(`^fun\s+([A-Za-z_$][A-Za-z0-9_$]*)\s*\(`)
	identifierPattern               = regexp.MustCompile(`[A-Za-z_$][A-Za-z0-9_$]*`)
)

func Build(files []document.File) []Edge {
	resolver := newResolver(files)
	edges := map[[2]int]bool{}
	definitions := map[string]map[int]bool{}

	for fileIndex, file := range files {
		language := languageForPath(file.Path)
		if language == "" {
			continue
		}
		lines := addedLines(file)
		for _, specifier := range importSpecifiers(language, lines) {
			for _, imported := range resolver.resolve(fileIndex, language, specifier) {
				if imported != fileIndex {
					edges[[2]int{fileIndex, imported}] = true
				}
			}
		}
		for _, line := range lines {
			if symbol := exportedDefinition(language, line); len(symbol) >= 4 {
				if definitions[symbol] == nil {
					definitions[symbol] = map[int]bool{}
				}
				definitions[symbol][fileIndex] = true
			}
		}
	}

	uniqueDefinitions := map[string]int{}
	for symbol, indexes := range definitions {
		if len(indexes) != 1 {
			continue
		}
		for index := range indexes {
			uniqueDefinitions[symbol] = index
		}
	}
	if len(uniqueDefinitions) > 0 {
		for fileIndex, file := range files {
			for _, line := range addedLines(file) {
				for _, symbol := range identifierPattern.FindAllString(line, -1) {
					definedAt, ok := uniqueDefinitions[symbol]
					if ok && definedAt != fileIndex {
						edges[[2]int{fileIndex, definedAt}] = true
					}
				}
			}
		}
	}

	result := make([]Edge, 0, len(edges))
	for pair := range edges {
		result = append(result, Edge{Importer: pair[0], Imported: pair[1]})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Importer != result[j].Importer {
			return result[i].Importer < result[j].Importer
		}
		return result[i].Imported < result[j].Imported
	})
	return result
}

func addedLines(file document.File) []string {
	lines := make([]string, 0, file.Additions)
	for _, hunk := range file.Hunks {
		for _, line := range hunk.Lines {
			if line.Type == "a" {
				lines = append(lines, line.Text)
			}
		}
	}
	return lines
}

func languageForPath(filePath string) string {
	switch strings.ToLower(path.Ext(filePath)) {
	case ".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs", ".mts", ".cts":
		return "javascript"
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".java":
		return "java"
	case ".kt", ".kts":
		return "kotlin"
	default:
		return ""
	}
}

func importSpecifiers(language string, lines []string) []string {
	var result []string
	if language == "javascript" {
		return javascriptImportSpecifiers(lines)
	}
	for _, line := range lines {
		result = append(result, lineImportSpecifiers(language, line)...)
	}
	return result
}

func lineImportSpecifiers(language, line string) []string {
	switch language {
	case "go":
		for _, pattern := range []*regexp.Regexp{goImportPattern, goImportSpecPattern} {
			if match := pattern.FindStringSubmatch(line); match != nil {
				return []string{match[1]}
			}
		}
	case "python":
		if match := pythonFromPattern.FindStringSubmatch(line); match != nil {
			return []string{match[1]}
		}
		if match := pythonImportPattern.FindStringSubmatch(line); match != nil {
			var result []string
			for _, item := range strings.Split(match[1], ",") {
				fields := strings.Fields(item)
				if len(fields) > 0 && pythonModulePattern.MatchString(fields[0]) {
					result = append(result, fields[0])
				}
			}
			return result
		}
	case "java", "kotlin":
		return jvmImportSpecifiers(language, line)
	}
	return nil
}

func jvmImportSpecifiers(language, line string) []string {
	staticImport := false
	target := ""
	if language == "java" {
		match := javaImportPattern.FindStringSubmatch(line)
		if match == nil {
			return nil
		}
		staticImport = match[1] != ""
		target = match[2]
	} else {
		match := kotlinImportPattern.FindStringSubmatch(line)
		if match == nil {
			return nil
		}
		target = match[1]
	}
	parts := strings.Split(target, ".")
	if staticImport {
		if len(parts) < 2 {
			return nil
		}
		parts = parts[:len(parts)-1]
	}
	return []string{strings.Join(parts, "/")}
}

func javascriptImportSpecifiers(lines []string) []string {
	var result []string
	collecting := false
	awaitingSpecifier := false
	awaitingFromLine := false
	braceDepth := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if isCommentOnly(trimmed) {
			continue
		}
		for _, match := range jsRequirePattern.FindAllStringSubmatch(line, -1) {
			result = append(result, match[1])
		}
		if collecting && awaitingFromLine {
			if jsFromLineStart.MatchString(line) {
				awaitingFromLine = false
			} else {
				collecting = false
				awaitingSpecifier = false
				awaitingFromLine = false
				braceDepth = 0
			}
		}
		startsStatement := jsImportStart.MatchString(line) || jsExportFromStart.MatchString(line)
		if startsStatement && collecting {
			collecting = false
			awaitingSpecifier = false
			awaitingFromLine = false
			braceDepth = 0
		}
		if startsStatement {
			if jsSideEffectImport.MatchString(line) {
				continue
			}
			collecting = true
			braceDepth = strings.Count(line, "{") - strings.Count(line, "}")
			awaitingFromLine = braceDepth == 0 && !jsFromKeyword.MatchString(line)
		}
		if !collecting {
			continue
		}
		if match := jsFromPattern.FindStringSubmatch(line); match != nil {
			result = append(result, match[1])
			collecting = false
			awaitingSpecifier = false
			awaitingFromLine = false
			braceDepth = 0
		} else if awaitingSpecifier {
			if match := jsQuotedSpecifier.FindStringSubmatch(line); match != nil {
				result = append(result, match[1])
				collecting = false
				awaitingSpecifier = false
				awaitingFromLine = false
				braceDepth = 0
			}
		} else if jsFromKeyword.MatchString(line) {
			awaitingSpecifier = true
		} else if strings.Contains(line, ";") {
			collecting = false
			awaitingSpecifier = false
			awaitingFromLine = false
			braceDepth = 0
		} else if !startsStatement {
			braceDepth += strings.Count(line, "{") - strings.Count(line, "}")
			if braceDepth == 0 && strings.Contains(line, "}") {
				awaitingFromLine = true
			}
		}
	}
	return result
}

func isCommentOnly(trimmed string) bool {
	return strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "/*") ||
		strings.HasPrefix(trimmed, "*") || strings.HasPrefix(trimmed, "*/")
}

func exportedDefinition(language, line string) string {
	var pattern *regexp.Regexp
	switch language {
	case "javascript":
		pattern = jsDefinitionPattern
	case "go":
		pattern = goDefinitionPattern
	case "python":
		pattern = pythonDefinitionPattern
	case "java":
		pattern = javaDefinitionPattern
	case "kotlin":
		for _, candidate := range []*regexp.Regexp{kotlinTypeDefinitionPattern, kotlinFunctionDefinitionPattern} {
			if match := candidate.FindStringSubmatch(line); match != nil {
				return match[1]
			}
		}
		return ""
	}
	if pattern == nil {
		return ""
	}
	match := pattern.FindStringSubmatch(line)
	if match == nil {
		return ""
	}
	return match[1]
}

type pathResolver struct {
	files             []document.File
	modules           map[string][]int
	directories       map[string][]int
	moduleSuffixes    map[string]suffixEntry
	directorySuffixes map[string]suffixEntry
	directoryFiles    map[string][]int
}

type suffixEntry struct {
	key       string
	ambiguous bool
}

func newResolver(files []document.File) pathResolver {
	resolver := pathResolver{
		files: files, modules: map[string][]int{}, directories: map[string][]int{},
		directoryFiles: map[string][]int{},
	}
	for index, file := range files {
		normalized := normalizePath(file.Path)
		withoutExtension := strings.TrimSuffix(normalized, path.Ext(normalized))
		resolver.addModule(normalized, index)
		resolver.addModule(withoutExtension, index)
		directory := path.Dir(normalized)
		resolver.directories[directory] = append(resolver.directories[directory], index)
		for _, suffix := range pathSuffixes(directory) {
			resolver.directoryFiles[suffix] = append(resolver.directoryFiles[suffix], index)
		}
		base := path.Base(withoutExtension)
		if base == "index" || base == "__init__" {
			resolver.addModule(path.Dir(withoutExtension), index)
		}
	}
	resolver.moduleSuffixes = buildSuffixIndex(resolver.modules)
	resolver.directorySuffixes = buildSuffixIndex(resolver.directories)
	return resolver
}

func (r pathResolver) addModule(module string, index int) {
	module = normalizePath(module)
	for _, existing := range r.modules[module] {
		if existing == index {
			return
		}
	}
	r.modules[module] = append(r.modules[module], index)
}

func (r pathResolver) resolve(importer int, language, specifier string) []int {
	if importer < 0 || importer >= len(r.files) {
		return nil
	}
	query := specifier
	switch language {
	case "javascript":
		query, _, _ = strings.Cut(query, "?")
		query, _, _ = strings.Cut(query, "#")
		if strings.HasPrefix(query, ".") {
			query = path.Join(path.Dir(normalizePath(r.files[importer].Path)), query)
		}
	case "python":
		var ok bool
		query, ok = r.pythonModule(importer, query)
		if !ok {
			return nil
		}
	case "java", "kotlin":
		query = strings.TrimPrefix(query, "/")
	default:
		query = strings.TrimPrefix(query, "/")
	}
	query = normalizePath(query)
	if query == ".." || strings.HasPrefix(query, "../") {
		return nil
	}
	if language == "go" {
		return r.resolveDirectory(query, language)
	}
	if isJVMLanguage(language) && strings.HasSuffix(query, "/*") {
		return r.resolveJVMDirectory(strings.TrimSuffix(query, "/*"), language)
	}
	return r.resolveModule(query, language)
}

func (r pathResolver) pythonModule(importer int, specifier string) (string, bool) {
	dots := 0
	for dots < len(specifier) && specifier[dots] == '.' {
		dots++
	}
	remainder := strings.ReplaceAll(specifier[dots:], ".", "/")
	if dots == 0 {
		return remainder, true
	}
	base := path.Dir(normalizePath(r.files[importer].Path))
	depth := 0
	if normalizedBase := normalizePath(base); normalizedBase != "" {
		depth = len(strings.Split(normalizedBase, "/"))
	}
	if dots-1 > depth {
		return "", false
	}
	for parent := 1; parent < dots; parent++ {
		base = path.Dir(base)
	}
	return path.Join(base, remainder), true
}

func (r pathResolver) resolveModule(query, language string) []int {
	if matches := uniqueSorted(r.modules[query]); len(matches) > 0 {
		return r.uniqueLanguageMatch(matches, language)
	}
	withoutExtension := strings.TrimSuffix(query, path.Ext(query))
	if withoutExtension != query {
		if matches := uniqueSorted(r.modules[withoutExtension]); len(matches) > 0 {
			return r.uniqueLanguageMatch(matches, language)
		}
	}
	return r.uniqueLanguageMatch(resolveUniqueSuffix(r.modules, r.moduleSuffixes, withoutExtension), language)
}

func (r pathResolver) resolveDirectory(query, language string) []int {
	if matches := uniqueSorted(r.directories[query]); len(matches) > 0 {
		return r.filterLanguage(matches, language)
	}
	return r.filterLanguage(resolveUniqueSuffix(r.directories, r.directorySuffixes, query), language)
}

func (r pathResolver) resolveJVMDirectory(query, language string) []int {
	return r.filterLanguage(r.directoryFiles[query], language)
}

func (r pathResolver) filterLanguage(indexes []int, language string) []int {
	result := make([]int, 0, len(indexes))
	for _, index := range indexes {
		if index >= 0 && index < len(r.files) &&
			languagesResolveTogether(language, languageForPath(r.files[index].Path)) {
			result = append(result, index)
		}
	}
	return result
}

func languagesResolveTogether(left, right string) bool {
	return left == right || isJVMLanguage(left) && isJVMLanguage(right)
}

func isJVMLanguage(language string) bool {
	return language == "java" || language == "kotlin"
}

func (r pathResolver) uniqueLanguageMatch(indexes []int, language string) []int {
	matches := r.filterLanguage(indexes, language)
	if len(matches) != 1 {
		return nil
	}
	return matches
}

func buildSuffixIndex(index map[string][]int) map[string]suffixEntry {
	result := map[string]suffixEntry{}
	for key := range index {
		for _, suffix := range pathSuffixes(key) {
			entry, exists := result[suffix]
			if !exists {
				result[suffix] = suffixEntry{key: key}
			} else if entry.key != key {
				entry.ambiguous = true
				result[suffix] = entry
			}
		}
	}
	return result
}

func resolveUniqueSuffix(index map[string][]int, suffixIndex map[string]suffixEntry, query string) []int {
	matchedKey := ""
	if entry, ok := suffixIndex[query]; ok {
		if entry.ambiguous {
			return nil
		}
		matchedKey = entry.key
	}
	for _, suffix := range pathSuffixes(query) {
		if _, ok := index[suffix]; !ok {
			continue
		}
		if matchedKey != "" && matchedKey != suffix {
			return nil
		}
		matchedKey = suffix
	}
	return uniqueSorted(index[matchedKey])
}

func pathSuffixes(value string) []string {
	parts := strings.Split(normalizePath(value), "/")
	result := make([]string, 0, len(parts))
	for index := range parts {
		suffix := strings.Join(parts[index:], "/")
		if suffix != "" {
			result = append(result, suffix)
		}
	}
	return result
}

func uniqueSorted(values []int) []int {
	if len(values) == 0 {
		return nil
	}
	seen := map[int]bool{}
	result := make([]int, 0, len(values))
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	sort.Ints(result)
	return result
}

func normalizePath(value string) string {
	value = strings.ReplaceAll(value, "\\", "/")
	value = strings.TrimPrefix(value, "./")
	value = strings.TrimPrefix(value, "/")
	cleaned := path.Clean(value)
	if cleaned == "." {
		return ""
	}
	return cleaned
}
