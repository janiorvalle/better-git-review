package changegraph

import (
	"path"
	"regexp"
	"strings"

	"github.com/janiorvalle/better-git-review/internal/document"
)

type terraformKey struct {
	kind     string
	typeName string
	name     string
}

type terraformFileFacts struct {
	directory  string
	defines    []terraformKey
	references []terraformKey
	sources    []string
}

var (
	terraformVariableBlockPattern = regexp.MustCompile(`^\s*variable\s+"([^"]+)"\s*\{`)
	terraformModuleBlockPattern   = regexp.MustCompile(`^\s*module\s+"([^"]+)"\s*\{`)
	terraformLocalsBlockPattern   = regexp.MustCompile(`^\s*locals\s*\{`)
	terraformResourceBlockPattern = regexp.MustCompile(`^\s*(resource|data)\s+"([^"]+)"\s+"([^"]+)"\s*\{`)
	terraformSourcePattern        = regexp.MustCompile(`^source\s*=\s*"([^"]+)"`)
	terraformHeredocStartPattern  = regexp.MustCompile(`<<-?\s*([A-Za-z_][A-Za-z0-9_]*)\s*$`)
	terraformVariableRefPattern   = regexp.MustCompile(`(?:^|[^A-Za-z0-9_.-])var\.([A-Za-z_][A-Za-z0-9_-]*)`)
	terraformModuleRefPattern     = regexp.MustCompile(`(?:^|[^A-Za-z0-9_.-])module\.([A-Za-z_][A-Za-z0-9_-]*)\.`)
	terraformLocalRefPattern      = regexp.MustCompile(`(?:^|[^A-Za-z0-9_.-])local\.([A-Za-z_][A-Za-z0-9_-]*)`)
	terraformDataRefPattern       = regexp.MustCompile(`(?:^|[^A-Za-z0-9_.-])data\.([A-Za-z_][A-Za-z0-9_-]*)\.([A-Za-z_][A-Za-z0-9_-]*)`)
	terraformResourceRefPattern   = regexp.MustCompile(`(?:^|[^A-Za-z0-9_.-])([A-Za-z_][A-Za-z0-9_-]*)\.([A-Za-z_][A-Za-z0-9_-]*)`)
)

type terraformTemplateState struct {
	depth             int
	inString          bool
	escaped           bool
	stringReturnDepth []int
}

func terraformEdges(files []document.File) []Edge {
	facts := make([]terraformFileFacts, len(files))
	definitions := map[string]map[terraformKey]map[int]bool{}
	terraFiles := map[string][]int{}
	for fileIndex, file := range files {
		if languageForPath(file.Path) != "terraform" {
			continue
		}
		fact := parseTerraformFile(file)
		facts[fileIndex] = fact
		terraFiles[fact.directory] = append(terraFiles[fact.directory], fileIndex)
		if definitions[fact.directory] == nil {
			definitions[fact.directory] = map[terraformKey]map[int]bool{}
		}
		for _, key := range fact.defines {
			if definitions[fact.directory][key] == nil {
				definitions[fact.directory][key] = map[int]bool{}
			}
			definitions[fact.directory][key][fileIndex] = true
		}
	}

	var result []Edge
	for fileIndex, fact := range facts {
		if languageForPath(files[fileIndex].Path) != "terraform" {
			continue
		}
		for _, reference := range fact.references {
			definers := definitions[fact.directory][reference]
			if len(definers) != 1 {
				continue
			}
			for definedAt := range definers {
				if definedAt != fileIndex {
					result = append(result, Edge{Importer: fileIndex, Imported: definedAt})
				}
			}
		}
		for _, source := range fact.sources {
			target, ok := terraformSourceDirectory(fact.directory, source)
			if !ok {
				continue
			}
			for _, imported := range terraFiles[target] {
				if imported != fileIndex {
					result = append(result, Edge{Importer: fileIndex, Imported: imported})
				}
			}
		}
	}
	return result
}

func parseTerraformFile(file document.File) terraformFileFacts {
	fact := terraformFileFacts{directory: normalizePath(path.Dir(normalizePath(file.Path)))}
	for _, run := range addedLineRuns(file) {
		parseTerraformRun(&fact, run)
	}
	return fact
}

func parseTerraformRun(fact *terraformFileFacts, lines []string) {
	depth := 0
	localsDepth := -1
	moduleDepth := -1
	inBlockComment := false
	heredocDelimiter := ""
	heredocTemplate := terraformTemplateState{}
	heredocTemplateBlockComment := false
	for _, rawLine := range lines {
		if heredocDelimiter != "" {
			if strings.TrimSpace(rawLine) == heredocDelimiter {
				heredocDelimiter = ""
				heredocTemplate = terraformTemplateState{}
				heredocTemplateBlockComment = false
				continue
			}
			template := terraformHeredocTemplate(rawLine, &heredocTemplate)
			template = maskTerraformStrings(template, false)
			template = stripTerraformComments(template, &heredocTemplateBlockComment)
			fact.references = append(fact.references, terraformReferences(template)...)
			continue
		}
		line := stripTerraformComments(rawLine, &inBlockComment)
		structure := maskTerraformStrings(line, false)

		if match := terraformVariableBlockPattern.FindStringSubmatch(line); match != nil {
			fact.defines = append(fact.defines, terraformKey{kind: "variable", name: match[1]})
		}
		if match := terraformModuleBlockPattern.FindStringSubmatch(line); match != nil {
			fact.defines = append(fact.defines, terraformKey{kind: "module", name: match[1]})
			moduleDepth = depth + 1
		}
		opensLocals := terraformLocalsBlockPattern.MatchString(line)
		if opensLocals {
			localsDepth = depth + 1
		}
		if match := terraformResourceBlockPattern.FindStringSubmatch(line); match != nil {
			fact.defines = append(fact.defines, terraformKey{kind: match[1], typeName: match[2], name: match[3]})
		}

		if moduleDepth >= 0 {
			if source := terraformDirectModuleSource(line, structure, depth, moduleDepth); source != "" {
				fact.sources = append(fact.sources, source)
			}
		}
		if localsDepth >= 0 {
			for _, name := range terraformDirectAssignments(structure, depth, localsDepth) {
				fact.defines = append(fact.defines, terraformKey{kind: "local", name: name})
			}
		}
		template := maskTerraformStrings(line, true)
		templateBlockComment := false
		template = stripTerraformComments(template, &templateBlockComment)
		fact.references = append(fact.references, terraformReferences(template)...)
		if match := terraformHeredocStartPattern.FindStringSubmatch(structure); match != nil {
			heredocDelimiter = match[1]
		}
		depth += strings.Count(structure, "{") - strings.Count(structure, "}")
		if localsDepth >= 0 && depth < localsDepth {
			localsDepth = -1
		}
		if moduleDepth >= 0 && depth < moduleDepth {
			moduleDepth = -1
		}
	}
}

func terraformDirectModuleSource(line, structure string, depth, moduleDepth int) string {
	for index := 0; index < len(structure); {
		switch structure[index] {
		case '{':
			depth++
			index++
			continue
		case '}':
			depth--
			index++
			continue
		}
		if depth != moduleDepth || !isTerraformIdentifierStart(structure[index]) {
			index++
			continue
		}
		end := index + 1
		for end < len(structure) && isTerraformIdentifierPart(structure[end]) {
			end++
		}
		if structure[index:end] == "source" {
			if match := terraformSourcePattern.FindStringSubmatch(line[index:]); match != nil {
				return match[1]
			}
		}
		index = end
	}
	return ""
}

func terraformDirectAssignments(line string, depth, localsDepth int) []string {
	var result []string
	for index := 0; index < len(line); {
		switch line[index] {
		case '{':
			depth++
			index++
			continue
		case '}':
			depth--
			index++
			continue
		}
		if depth != localsDepth || !isTerraformIdentifierStart(line[index]) {
			index++
			continue
		}
		end := index + 1
		for end < len(line) && isTerraformIdentifierPart(line[end]) {
			end++
		}
		equals := end
		for equals < len(line) && (line[equals] == ' ' || line[equals] == '\t') {
			equals++
		}
		if equals < len(line) && line[equals] == '=' && (equals+1 == len(line) || line[equals+1] != '=') {
			result = append(result, line[index:end])
		}
		index = end
	}
	return result
}

func isTerraformIdentifierStart(char byte) bool {
	return char == '_' || char >= 'A' && char <= 'Z' || char >= 'a' && char <= 'z'
}

func isTerraformIdentifierPart(char byte) bool {
	return isTerraformIdentifierStart(char) || char == '-' || char >= '0' && char <= '9'
}

func terraformHeredocTemplate(line string, state *terraformTemplateState) string {
	var result strings.Builder
	for index := 0; index < len(line); index++ {
		char := line[index]
		if state.depth == 0 {
			if (char == '$' || char == '%') && index+2 < len(line) && line[index+1] == char && line[index+2] == '{' {
				index += 2
				continue
			}
			if (char == '$' || char == '%') && index+1 < len(line) && line[index+1] == '{' {
				state.depth = 1
				index++
			}
			continue
		}
		if state.inString {
			if state.escaped {
				state.escaped = false
			} else if char == '\\' {
				state.escaped = true
			} else if (char == '$' || char == '%') && index+2 < len(line) && line[index+1] == char && line[index+2] == '{' {
				index += 2
			} else if (char == '$' || char == '%') && index+1 < len(line) && line[index+1] == '{' {
				state.stringReturnDepth = append(state.stringReturnDepth, state.depth)
				state.depth++
				state.inString = false
				index++
			} else if char == '"' {
				state.inString = false
			}
			result.WriteByte(' ')
			continue
		}
		if char == '"' {
			state.inString = true
			result.WriteByte(' ')
			continue
		}
		switch char {
		case '{':
			state.depth++
			result.WriteByte(char)
		case '}':
			state.depth--
			if len(state.stringReturnDepth) > 0 && state.depth == state.stringReturnDepth[len(state.stringReturnDepth)-1] {
				state.stringReturnDepth = state.stringReturnDepth[:len(state.stringReturnDepth)-1]
				state.inString = true
			}
			result.WriteByte(' ')
		default:
			result.WriteByte(char)
		}
	}
	result.WriteByte('\n')
	return result.String()
}

func stripTerraformComments(line string, inBlock *bool) string {
	var result strings.Builder
	inString := false
	escaped := false
	for index := 0; index < len(line); index++ {
		char := line[index]
		if *inBlock {
			if char == '*' && index+1 < len(line) && line[index+1] == '/' {
				*inBlock = false
				result.WriteString("  ")
				index++
			} else {
				result.WriteByte(' ')
			}
			continue
		}
		if inString {
			result.WriteByte(char)
			if escaped {
				escaped = false
			} else if char == '\\' {
				escaped = true
			} else if char == '"' {
				inString = false
			}
			continue
		}
		if char == '"' {
			inString = true
			result.WriteByte(char)
			continue
		}
		if char == '#' || char == '/' && index+1 < len(line) && line[index+1] == '/' {
			break
		}
		if char == '/' && index+1 < len(line) && line[index+1] == '*' {
			*inBlock = true
			result.WriteString("  ")
			index++
			continue
		}
		result.WriteByte(char)
	}
	return result.String()
}

func maskTerraformStrings(line string, preserveTemplates bool) string {
	var result strings.Builder
	inString := false
	escaped := false
	templateDepth := 0
	templateInString := false
	templateEscaped := false
	var templateStringReturnDepth []int
	for index := 0; index < len(line); index++ {
		char := line[index]
		if !inString {
			if char == '"' {
				inString = true
				result.WriteByte(' ')
			} else {
				result.WriteByte(char)
			}
			continue
		}
		if templateDepth > 0 {
			if templateInString {
				if templateEscaped {
					templateEscaped = false
				} else if char == '\\' {
					templateEscaped = true
				} else if preserveTemplates && (char == '$' || char == '%') && index+2 < len(line) && line[index+1] == char && line[index+2] == '{' {
					index += 2
				} else if preserveTemplates && (char == '$' || char == '%') && index+1 < len(line) && line[index+1] == '{' {
					templateStringReturnDepth = append(templateStringReturnDepth, templateDepth)
					templateDepth++
					templateInString = false
					index++
				} else if char == '"' {
					templateInString = false
				}
				result.WriteByte(' ')
				continue
			}
			if char == '"' {
				templateInString = true
				result.WriteByte(' ')
				continue
			}
			switch char {
			case '{':
				templateDepth++
				result.WriteByte(char)
			case '}':
				templateDepth--
				if len(templateStringReturnDepth) > 0 && templateDepth == templateStringReturnDepth[len(templateStringReturnDepth)-1] {
					templateStringReturnDepth = templateStringReturnDepth[:len(templateStringReturnDepth)-1]
					templateInString = true
				}
				result.WriteByte(' ')
			default:
				result.WriteByte(char)
			}
			continue
		}
		if escaped {
			escaped = false
			result.WriteByte(' ')
			continue
		}
		if char == '\\' {
			escaped = true
			result.WriteByte(' ')
			continue
		}
		if char == '"' {
			inString = false
			result.WriteByte(' ')
			continue
		}
		if preserveTemplates && (char == '$' || char == '%') && index+2 < len(line) && line[index+1] == char && line[index+2] == '{' {
			result.WriteString("   ")
			index += 2
			continue
		}
		if preserveTemplates && (char == '$' || char == '%') && index+1 < len(line) && line[index+1] == '{' {
			templateDepth = 1
			result.WriteString("  ")
			index++
			continue
		}
		result.WriteByte(' ')
	}
	return result.String()
}

func terraformReferences(line string) []terraformKey {
	var result []terraformKey
	for _, match := range terraformVariableRefPattern.FindAllStringSubmatch(line, -1) {
		result = append(result, terraformKey{kind: "variable", name: match[1]})
	}
	for _, match := range terraformModuleRefPattern.FindAllStringSubmatch(line, -1) {
		result = append(result, terraformKey{kind: "module", name: match[1]})
	}
	for _, match := range terraformLocalRefPattern.FindAllStringSubmatch(line, -1) {
		result = append(result, terraformKey{kind: "local", name: match[1]})
	}
	for _, match := range terraformDataRefPattern.FindAllStringSubmatch(line, -1) {
		result = append(result, terraformKey{kind: "data", typeName: match[1], name: match[2]})
	}
	for _, match := range terraformResourceRefPattern.FindAllStringSubmatch(line, -1) {
		switch match[1] {
		case "var", "local", "module", "data":
			continue
		}
		result = append(result, terraformKey{kind: "resource", typeName: match[1], name: match[2]})
	}
	return result
}

func terraformSourceDirectory(directory, source string) (string, bool) {
	if !strings.HasPrefix(source, "./") && !strings.HasPrefix(source, "../") {
		return "", false
	}
	target := normalizePath(path.Join(directory, source))
	if target == ".." || strings.HasPrefix(target, "../") {
		return "", false
	}
	return target, true
}
