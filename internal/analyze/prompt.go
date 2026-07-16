package analyze

import (
	"fmt"
	"strings"

	"github.com/janiorvalle/better-git-review/internal/document"
)

const (
	totalPromptDiffCap = 160_000
	maxFileDiffCap     = 12_000
)

func BuildPrompt(source document.Source, files []document.File) string {
	perFileCap := totalPromptDiffCap / max(len(files), 1)
	perFileCap = min(max(perFileCap, 256), maxFileDiffCap)

	var filesBlock strings.Builder
	for index, file := range files {
		fmt.Fprintf(&filesBlock, "\n===== FILE %d: %s (%s, +%d/-%d) =====\n",
			index, file.Path, file.Status, file.Additions, file.Deletions)
		filesBlock.WriteString(fileDiffText(file, perFileCap))
	}
	filesText := filesBlock.String()
	if len(filesText) > totalPromptDiffCap {
		filesText = filesText[:totalPromptDiffCap] + "\n... [total diff truncated]\n"
	}

	description := ""
	if source.Description != "" {
		value := source.Description
		if len(value) > 3_000 {
			value = value[:3_000] + "\n... [description truncated]"
		}
		description = "DESCRIPTION:\n" + value + "\n"
	}
	return fmt.Sprintf(`You are an expert code-review guide. Analyze this diff and organize it into a guided walkthrough for a human reviewer.

CHANGE: %s
%sFILES CHANGED: %d
%s

Group the changed files into intent-based cohorts (a cohort = a set of files serving one purpose). Order cohorts in the most logical reading order for a reviewer, typically schema/data model -> backend logic -> API surface -> UI -> tests -> config/docs. Every file index must appear in exactly one cohort.

Respond with ONLY a JSON object, with no markdown fences or prose, in exactly this shape:
{
  "title": "short human title for the overall change",
  "overview": "2-4 sentence plain-language summary",
  "mermaid": "small graph LR or graph TD diagram, or null",
  "cohorts": [{
    "title": "short cohort title",
    "layer": "schema | backend | api | ui | tests | config | docs | other",
    "intent": "one sentence describing this group's purpose",
    "narrative": "2-5 sentences guiding the reviewer through the change",
    "files": [0, 2],
    "fileSummaries": ["summary parallel to file 0", "summary parallel to file 2"],
    "reviewNotes": ["specific risks or checks, or an empty array"],
    "dependsOn": [0]
  }]
}

dependsOn may reference only earlier cohort indexes.`, source.Title, description, len(files), filesText)
}

func fileDiffText(file document.File, cap int) string {
	if file.Binary {
		return "(binary file)\n"
	}
	var output strings.Builder
	for _, hunk := range file.Hunks {
		fmt.Fprintf(&output, "@@ %s\n", hunk.Header)
		for _, line := range hunk.Lines {
			prefix := " "
			if line.Type == "a" {
				prefix = "+"
			} else if line.Type == "d" {
				prefix = "-"
			}
			output.WriteString(prefix)
			output.WriteString(line.Text)
			output.WriteByte('\n')
		}
	}
	value := output.String()
	if len(value) > cap {
		return value[:cap] + "\n... [truncated]\n"
	}
	return value
}
