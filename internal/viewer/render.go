package viewer

import (
	"bytes"
	_ "embed"
	"fmt"
	"html/template"
	"io"

	"github.com/janiorvalle/better-git-review/internal/document"
	"github.com/janiorvalle/better-git-review/internal/media"
)

//go:embed template.html
var templateSource string

var viewerTemplate = template.Must(template.New("viewer").Parse(templateSource))

func Render(doc document.Document) ([]byte, error) {
	return RenderWithPreviews(doc, nil)
}

func RenderWithPreviews(doc document.Document, previews map[int]media.Preview) ([]byte, error) {
	var output bytes.Buffer
	if err := RenderToWithPreviews(&output, doc, previews); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func RenderToWithPreviews(output io.Writer, doc document.Document, previews map[int]media.Preview) error {
	page, err := buildPageWithPreviews(doc, previews)
	if err != nil {
		return err
	}
	if err := viewerTemplate.Execute(output, page); err != nil {
		return fmt.Errorf("render viewer: %w", err)
	}
	return nil
}
