package analyze

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	"github.com/janiorvalle/better-git-review/internal/document"
)

func ParseResponse(text string) (document.Analysis, error) {
	var analysis document.Analysis
	if err := ParseResponseInto(text, &analysis); err != nil {
		return document.Analysis{}, err
	}
	return analysis, nil
}

func ParseResponseInto(text string, target any) error {
	candidate, err := ExtractJSON(text)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(candidate, target); err == nil {
		return nil
	}
	repaired := RepairJSON(string(candidate))
	if err := json.Unmarshal([]byte(repaired), target); err != nil {
		return fmt.Errorf("parse JSON after repair: %w", err)
	}
	return nil
}

func ExtractJSON(text string) ([]byte, error) {
	if start := strings.Index(text, "```"); start >= 0 {
		rest := text[start+3:]
		if newline := strings.IndexByte(rest, '\n'); newline >= 0 {
			rest = rest[newline+1:]
		}
		if end := strings.Index(rest, "```"); end >= 0 {
			text = rest[:end]
		}
	}
	objectStart := strings.IndexByte(text, '{')
	arrayStart := strings.IndexByte(text, '[')
	start, closing := objectStart, byte('}')
	if arrayStart >= 0 && (objectStart < 0 || arrayStart < objectStart) {
		start, closing = arrayStart, ']'
	}
	end := strings.LastIndexByte(text, closing)
	if start < 0 || end <= start {
		return nil, fmt.Errorf("response did not contain a JSON object or array")
	}
	return []byte(text[start : end+1]), nil
}

// RepairJSON targets the two failures observed in model output: raw control
// characters and unescaped quotes inside string values.
func RepairJSON(input string) string {
	var output bytes.Buffer
	inString := false
	for i := 0; i < len(input); i++ {
		current := input[i]
		if !inString {
			if current == '"' {
				inString = true
			}
			output.WriteByte(current)
			continue
		}
		if current == '\\' {
			output.WriteByte(current)
			if i+1 < len(input) {
				i++
				output.WriteByte(input[i])
			}
			continue
		}
		switch current {
		case '\n':
			output.WriteString(`\n`)
			continue
		case '\r':
			continue
		case '\t':
			output.WriteString(`\t`)
			continue
		case '"':
			next := nextNonSpace(input, i+1)
			if next == 0 || next == ',' || next == '}' || next == ']' || next == ':' {
				inString = false
				output.WriteByte(current)
			} else {
				output.WriteString(`\"`)
			}
			continue
		}
		output.WriteByte(current)
	}
	return output.String()
}

func nextNonSpace(value string, start int) byte {
	for i := start; i < len(value); i++ {
		if !unicode.IsSpace(rune(value[i])) {
			return value[i]
		}
	}
	return 0
}
