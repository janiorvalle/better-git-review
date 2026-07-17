package viewer

import (
	"encoding/json"
	"html/template"
	"testing"

	"github.com/janiorvalle/better-git-review/internal/document"
)

func TestCompactDiffJSONRoundTripShape(t *testing.T) {
	value, err := compactDiffJSON([]document.File{{
		Path: "main.go",
		Hunks: []document.Hunk{{
			Header: "func main",
			Blame:  &document.Blame{Author: "Ada", Date: "2026-07-17"},
			Lines: []document.HunkLine{{
				Type: "d", Old: 4, Text: "old",
			}, {
				Type: "a", New: 4, Text: "new",
			}},
		}},
	}}, []FileView{{
		Lang: "GO", BinaryLabel: "Binary image",
		OldImage: &ImageAssetView{
			DataURI:   template.URL("data:image/png;base64,b2xk"),
			SizeLabel: "3 B", Dimensions: "1x1",
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Files [][][]any `json:"f"`
		UI    [][]any   `json:"u"`
	}
	if err := json.Unmarshal([]byte(value), &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Files) != 1 || len(decoded.Files[0]) != 1 {
		t.Fatalf("payload shape = %#v", decoded)
	}
	hunk := decoded.Files[0][0]
	if hunk[0] != "func main" || hunk[1] != "Ada" || hunk[2] != "2026-07-17" {
		t.Fatalf("hunk metadata = %#v", hunk)
	}
	if len(decoded.UI) != 1 || decoded.UI[0][0] != "GO" ||
		decoded.UI[0][1] != "Binary image" {
		t.Fatalf("file UI metadata = %#v", decoded.UI)
	}
	oldImage, ok := decoded.UI[0][2].([]any)
	if !ok || len(oldImage) != 3 ||
		oldImage[0] != "data:image/png;base64,b2xk" ||
		oldImage[1] != "3 B" || oldImage[2] != "1x1" {
		t.Fatalf("old image payload = %#v", decoded.UI[0][2])
	}
}
