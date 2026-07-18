package changegraph

import (
	"regexp"
	"strings"
)

var (
	htmlAssetTagPattern    = regexp.MustCompile(`(?is)<(script|style|link|img)\b(?:[^>"']|"[^"]*"|'[^']*')*>`)
	htmlScriptClosePattern = regexp.MustCompile(`(?is)</script\s*>`)
	htmlStyleClosePattern  = regexp.MustCompile(`(?is)</style\s*>`)
	cssImportPattern       = regexp.MustCompile(`(?i)^@import\s+(?:"([^"]+)"|'([^']+)')`)
	cssURLPattern          = regexp.MustCompile(`(?i)^url\(\s*(?:"([^"]*)"|'([^']*)'|([^)"'\s]+))\s*\)`)
	referenceSchemePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*:`)
)

func assetSpecifiers(language, line string) []string {
	switch language {
	case "html":
		line = stripHTMLComments(line)
		return htmlAssetReferences(line)
	case "css":
		line = stripCSSComments(line)
		return cssAssetReferences(line)
	default:
		return nil
	}
}

func htmlAssetReferences(value string) []string {
	var result []string
	for len(value) > 0 {
		match := htmlAssetTagPattern.FindStringSubmatchIndex(value)
		if match == nil {
			break
		}
		tagName := value[match[2]:match[3]]
		tag := value[match[0]:match[1]]
		attribute := "src"
		if strings.EqualFold(tagName, "link") {
			attribute = "href"
		}
		if !strings.EqualFold(tagName, "style") {
			if reference := htmlAttribute(tag, attribute); isRelativeAssetReference(reference) {
				result = append(result, reference)
			}
		}
		value = value[match[1]:]
		var closePattern *regexp.Regexp
		if strings.EqualFold(tagName, "script") {
			closePattern = htmlScriptClosePattern
		} else if strings.EqualFold(tagName, "style") {
			closePattern = htmlStyleClosePattern
		}
		if closePattern != nil {
			close := closePattern.FindStringIndex(value)
			if close == nil {
				break
			}
			value = value[close[1]:]
		}
	}
	return result
}

func cssAssetReferences(value string) []string {
	var result []string
	quote := byte(0)
	escaped := false
	for index := 0; index < len(value); {
		if quote != 0 {
			char := value[index]
			index++
			if escaped {
				escaped = false
			} else if char == '\\' {
				escaped = true
			} else if char == quote {
				quote = 0
			}
			continue
		}
		matched := false
		for _, pattern := range []*regexp.Regexp{cssImportPattern, cssURLPattern} {
			match := pattern.FindStringSubmatch(value[index:])
			if match == nil {
				continue
			}
			if reference := firstNonEmpty(match[1:]); isRelativeAssetReference(reference) {
				result = append(result, reference)
			}
			index += len(match[0])
			matched = true
			break
		}
		if matched {
			continue
		}
		if value[index] == '"' || value[index] == '\'' {
			quote = value[index]
		}
		index++
	}
	return result
}

func htmlAttribute(tag, wanted string) string {
	index := 1
	for index < len(tag) && isHTMLNameChar(tag[index]) {
		index++
	}
	for index < len(tag) {
		for index < len(tag) && (tag[index] == ' ' || tag[index] == '\t' || tag[index] == '\r' || tag[index] == '\n' || tag[index] == '/') {
			index++
		}
		if index >= len(tag) || tag[index] == '>' {
			return ""
		}
		start := index
		for index < len(tag) && isHTMLNameChar(tag[index]) {
			index++
		}
		if start == index {
			index++
			continue
		}
		name := tag[start:index]
		for index < len(tag) && (tag[index] == ' ' || tag[index] == '\t' || tag[index] == '\r' || tag[index] == '\n') {
			index++
		}
		if index >= len(tag) || tag[index] != '=' {
			continue
		}
		index++
		for index < len(tag) && (tag[index] == ' ' || tag[index] == '\t' || tag[index] == '\r' || tag[index] == '\n') {
			index++
		}
		valueStart := index
		if index < len(tag) && (tag[index] == '"' || tag[index] == '\'') {
			quote := tag[index]
			index++
			valueStart = index
			for index < len(tag) && tag[index] != quote {
				index++
			}
			if strings.EqualFold(name, wanted) {
				return tag[valueStart:index]
			}
			if index < len(tag) {
				index++
			}
			continue
		}
		for index < len(tag) && tag[index] != ' ' && tag[index] != '\t' && tag[index] != '\r' && tag[index] != '\n' && tag[index] != '>' {
			index++
		}
		if strings.EqualFold(name, wanted) {
			return tag[valueStart:index]
		}
	}
	return ""
}

func isHTMLNameChar(char byte) bool {
	return char == '-' || char == ':' || char == '_' || char >= '0' && char <= '9' || char >= 'A' && char <= 'Z' || char >= 'a' && char <= 'z'
}

func stripHTMLComments(value string) string {
	var result strings.Builder
	inTag := false
	quote := byte(0)
	for index := 0; index < len(value); {
		if !inTag && strings.HasPrefix(value[index:], "<!--") {
			end := strings.Index(value[index+4:], "-->")
			if end < 0 {
				return result.String()
			}
			length := 4 + end + 3
			result.WriteString(strings.Repeat(" ", length))
			index += length
			continue
		}
		char := value[index]
		result.WriteByte(char)
		index++
		if !inTag {
			if char == '<' {
				inTag = true
			}
			continue
		}
		if quote != 0 {
			if char == quote {
				quote = 0
			}
			continue
		}
		if char == '"' || char == '\'' {
			quote = char
		} else if char == '>' {
			inTag = false
		}
	}
	return result.String()
}

func stripCSSComments(value string) string {
	var result strings.Builder
	quote := byte(0)
	escaped := false
	for index := 0; index < len(value); {
		if quote != 0 {
			char := value[index]
			result.WriteByte(char)
			index++
			if escaped {
				escaped = false
			} else if char == '\\' {
				escaped = true
			} else if char == quote {
				quote = 0
			}
			continue
		}
		if value[index] == '"' || value[index] == '\'' {
			quote = value[index]
			result.WriteByte(value[index])
			index++
			continue
		}
		if strings.HasPrefix(value[index:], "/*") {
			end := strings.Index(value[index+2:], "*/")
			if end < 0 {
				return result.String()
			}
			length := 2 + end + 2
			result.WriteString(strings.Repeat(" ", length))
			index += length
			continue
		}
		result.WriteByte(value[index])
		index++
	}
	return result.String()
}

func multilineAssetSpecifiers(language string, runs [][]string) []string {
	var result []string
	for _, lines := range runs {
		result = append(result, assetSpecifiers(language, strings.Join(lines, "\n"))...)
	}
	return result
}

func firstNonEmpty(values []string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func isRelativeAssetReference(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && !strings.HasPrefix(value, "/") &&
		!strings.HasPrefix(value, "#") && !referenceSchemePattern.MatchString(value)
}
