package viewer

import (
	"bytes"
	_ "embed"
	"fmt"
	"html/template"

	"github.com/janiorvalle/better-git-review/internal/document"
)

//go:embed template.html
var templateSource string

var viewerTemplate = template.Must(template.New("viewer").Parse(templateSource))

func Render(doc document.Document) ([]byte, error) {
	page, err := buildPage(doc)
	if err != nil {
		return nil, err
	}
	var output bytes.Buffer
	if err := viewerTemplate.Execute(&output, page); err != nil {
		return nil, fmt.Errorf("render viewer: %w", err)
	}
	return output.Bytes(), nil
}
