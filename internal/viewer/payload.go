package viewer

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/janiorvalle/better-git-review/internal/document"
)

// compactDiffJSON is the canonical diff-content encoding for staged HTML
// artifacts. The public --format json document remains unchanged; this
// private array format minimizes serialized overhead before JavaScript stamps
// unified and split rows into the live DOM.
func compactDiffJSON(files []document.File, views []FileView) (string, error) {
	var output bytes.Buffer
	output.WriteString(`{"f":[`)
	for fileIndex, file := range files {
		if fileIndex > 0 {
			output.WriteByte(',')
		}
		output.WriteByte('[')
		for hunkIndex, hunk := range file.Hunks {
			if hunkIndex > 0 {
				output.WriteByte(',')
			}
			output.WriteByte('[')
			if err := writeJSONString(&output, hunkLabel(hunk)); err != nil {
				return "", err
			}
			output.WriteByte(',')
			if hunk.Blame == nil {
				output.WriteString(`"","",`)
			} else {
				if err := writeJSONString(&output, hunk.Blame.Author); err != nil {
					return "", err
				}
				output.WriteByte(',')
				if err := writeJSONString(&output, hunk.Blame.Date); err != nil {
					return "", err
				}
				output.WriteByte(',')
			}
			output.WriteByte('[')
			for lineIndex, line := range hunk.Lines {
				if lineIndex > 0 {
					output.WriteByte(',')
				}
				output.WriteByte('[')
				if err := writeJSONString(&output, line.Type); err != nil {
					return "", err
				}
				fmt.Fprintf(&output, ",%d,%d,", line.Old, line.New)
				if err := writeJSONString(&output, line.Text); err != nil {
					return "", err
				}
				if isLongLine(line.Text) {
					output.WriteString(`,1]`)
				} else {
					output.WriteString(`,0]`)
				}
			}
			output.WriteString(`]]`)
		}
		output.WriteByte(']')
	}
	output.WriteString(`],"u":[`)
	for index := range files {
		if index > 0 {
			output.WriteByte(',')
		}
		view := views[index]
		output.WriteByte('[')
		if err := writeJSONString(&output, view.Lang); err != nil {
			return "", err
		}
		output.WriteByte(',')
		if err := writeJSONString(&output, view.BinaryLabel); err != nil {
			return "", err
		}
		output.WriteByte(',')
		if err := writeImageAsset(&output, view.OldImage); err != nil {
			return "", err
		}
		output.WriteByte(',')
		if err := writeImageAsset(&output, view.NewImage); err != nil {
			return "", err
		}
		output.WriteByte(']')
	}
	output.WriteString(`]}`)
	return output.String(), nil
}

func writeJSONString(output *bytes.Buffer, value string) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	output.Write(encoded)
	return nil
}

func writeImageAsset(output *bytes.Buffer, asset *ImageAssetView) error {
	if asset == nil {
		output.WriteString("null")
		return nil
	}
	output.WriteByte('[')
	for index, value := range []string{string(asset.DataURI), asset.SizeLabel, asset.Dimensions} {
		if index > 0 {
			output.WriteByte(',')
		}
		if err := writeJSONString(output, value); err != nil {
			return err
		}
	}
	output.WriteByte(']')
	return nil
}
