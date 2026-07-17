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
	return RenderWithSettings(doc, DefaultSettings())
}

func RenderWithSettings(doc document.Document, settings Settings) ([]byte, error) {
	return RenderWithPreviewsAndSettings(doc, nil, settings)
}

func RenderWithPreviews(doc document.Document, previews map[int]media.Preview) ([]byte, error) {
	return RenderWithPreviewsAndSettings(doc, previews, DefaultSettings())
}

func RenderWithPreviewsAndSettings(doc document.Document, previews map[int]media.Preview, settings Settings) ([]byte, error) {
	var output bytes.Buffer
	if err := RenderToWithPreviewsAndSettings(&output, doc, previews, settings); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func RenderToWithPreviews(output io.Writer, doc document.Document, previews map[int]media.Preview) error {
	return RenderToWithPreviewsAndSettings(output, doc, previews, DefaultSettings())
}

func RenderToWithPreviewsAndSettings(output io.Writer, doc document.Document, previews map[int]media.Preview, settings Settings) error {
	page, err := buildPageWithPreviewsAndSettings(doc, previews, settings)
	if err != nil {
		return err
	}
	if err := viewerTemplate.Execute(output, page); err != nil {
		return fmt.Errorf("render viewer: %w", err)
	}
	return nil
}
