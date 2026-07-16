package diff

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/janiorvalle/better-git-review/internal/document"
)

func Parse(text string) ([]document.File, error) {
	var files []document.File
	var file *document.File
	var hunk *document.Hunk
	oldLine, newLine := 0, 0

	pushFile := func() {
		if file != nil {
			files = append(files, *file)
		}
		file = nil
		hunk = nil
	}

	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "diff --git ") {
			oldPath, newPath, err := parseDiffHeader(strings.TrimPrefix(line, "diff --git "))
			if err != nil {
				return nil, err
			}
			pushFile()
			file = &document.File{
				Path:    trimSidePrefix(newPath, "b/"),
				OldPath: trimSidePrefix(oldPath, "a/"),
				NewPath: trimSidePrefix(newPath, "b/"),
				Status:  "modified",
				Hunks:   []document.Hunk{},
			}
			continue
		}
		if file == nil {
			continue
		}

		switch {
		case strings.HasPrefix(line, "new file mode "):
			file.Status = "added"
			continue
		case strings.HasPrefix(line, "deleted file mode "):
			file.Status = "deleted"
			file.Path = file.OldPath
			continue
		case strings.HasPrefix(line, "rename from "):
			file.Status = "renamed"
			file.OldPath = parseMetadataPath(strings.TrimPrefix(line, "rename from "))
			continue
		case strings.HasPrefix(line, "rename to "):
			file.NewPath = parseMetadataPath(strings.TrimPrefix(line, "rename to "))
			file.Path = file.NewPath
			continue
		case strings.HasPrefix(line, "Binary files "), line == "GIT binary patch":
			file.Binary = true
			continue
		case hunk == nil && isMetadataLine(line):
			continue
		}

		if oldStart, newStart, header, ok := parseHunkHeader(line); ok {
			oldLine, newLine = oldStart, newStart
			file.Hunks = append(file.Hunks, document.Hunk{Header: header, Lines: []document.HunkLine{}})
			hunk = &file.Hunks[len(file.Hunks)-1]
			continue
		}
		if hunk == nil {
			continue
		}

		switch {
		case strings.HasPrefix(line, "+"):
			hunk.Lines = append(hunk.Lines, document.HunkLine{Type: "a", New: newLine, Text: line[1:]})
			newLine++
			file.Additions++
		case strings.HasPrefix(line, "-"):
			hunk.Lines = append(hunk.Lines, document.HunkLine{Type: "d", Old: oldLine, Text: line[1:]})
			oldLine++
			file.Deletions++
		case strings.HasPrefix(line, " "):
			hunk.Lines = append(hunk.Lines, document.HunkLine{Type: "c", Old: oldLine, New: newLine, Text: line[1:]})
			oldLine++
			newLine++
		case strings.HasPrefix(line, `\ No newline at end of file`):
			continue
		}
	}
	pushFile()
	return files, nil
}

func parseDiffHeader(header string) (string, string, error) {
	if strings.HasPrefix(header, "a/") {
		var separators []int
		for offset := 0; offset < len(header); {
			relative := strings.Index(header[offset:], " b/")
			if relative < 0 {
				break
			}
			separator := offset + relative
			separators = append(separators, separator)
			oldPath := strings.TrimPrefix(header[:separator], "a/")
			newPath := strings.TrimPrefix(header[separator+1:], "b/")
			if oldPath == newPath {
				return header[:separator], header[separator+1:], nil
			}
			offset = separator + 3
		}
		if len(separators) > 0 {
			separator := separators[0]
			return header[:separator], header[separator+1:], nil
		}
	}
	fields, err := splitGitFields(header)
	if err != nil {
		return "", "", fmt.Errorf("parse diff header %q: %w", header, err)
	}
	if len(fields) != 2 {
		return "", "", fmt.Errorf("parse diff header %q: expected two paths", header)
	}
	return fields[0], fields[1], nil
}

func splitGitFields(value string) ([]string, error) {
	var fields []string
	for i := 0; i < len(value); {
		for i < len(value) && unicode.IsSpace(rune(value[i])) {
			i++
		}
		if i >= len(value) {
			break
		}
		if value[i] == '"' {
			start := i
			i++
			escaped := false
			for i < len(value) {
				if value[i] == '"' && !escaped {
					i++
					break
				}
				if value[i] == '\\' && !escaped {
					escaped = true
				} else {
					escaped = false
				}
				i++
			}
			if i > len(value) || value[i-1] != '"' {
				return nil, fmt.Errorf("unterminated quoted path")
			}
			unquoted, err := strconv.Unquote(value[start:i])
			if err != nil {
				return nil, err
			}
			fields = append(fields, unquoted)
			continue
		}
		start := i
		for i < len(value) && !unicode.IsSpace(rune(value[i])) {
			i++
		}
		fields = append(fields, value[start:i])
	}
	return fields, nil
}

func parseMetadataPath(path string) string {
	path = strings.TrimSpace(path)
	if strings.HasPrefix(path, `"`) {
		if unquoted, err := strconv.Unquote(path); err == nil {
			return unquoted
		}
	}
	return path
}

func trimSidePrefix(path, prefix string) string {
	return strings.TrimPrefix(path, prefix)
}

func isMetadataLine(line string) bool {
	prefixes := []string{
		"--- ", "+++ ", "index ", "old mode ", "new mode ",
		"similarity index ", "dissimilarity index ",
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}
	return false
}

func parseHunkHeader(line string) (oldStart, newStart int, header string, ok bool) {
	if !strings.HasPrefix(line, "@@ -") {
		return 0, 0, "", false
	}
	end := strings.Index(line[3:], " @@")
	if end < 0 {
		return 0, 0, "", false
	}
	rangePart := line[3 : 3+end]
	var oldRange, newRange string
	if _, err := fmt.Sscanf(rangePart, "%s %s", &oldRange, &newRange); err != nil {
		return 0, 0, "", false
	}
	oldStart, errOld := rangeStart(strings.TrimPrefix(oldRange, "-"))
	newStart, errNew := rangeStart(strings.TrimPrefix(newRange, "+"))
	if errOld != nil || errNew != nil {
		return 0, 0, "", false
	}
	header = strings.TrimSpace(line[3+end+3:])
	return oldStart, newStart, header, true
}

func rangeStart(value string) (int, error) {
	if comma := strings.IndexByte(value, ','); comma >= 0 {
		value = value[:comma]
	}
	return strconv.Atoi(value)
}
