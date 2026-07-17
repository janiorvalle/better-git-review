package picker

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

func TestFilterSearchesFullCatalog(t *testing.T) {
	catalog := Catalog{PRs: []Item{
		{Kind: PR, Label: "#12 Fix auth", Search: "12 Fix auth"},
		{Kind: PR, Label: "#13 Viewer polish", Search: "13 Viewer polish"},
	}}
	filtered := Filter(catalog, "AUTH")
	if len(filtered.PRs) != 1 || filtered.PRs[0].Label != "#12 Fix auth" {
		t.Fatalf("filtered = %#v", filtered.PRs)
	}
}

func TestRenderCapsSectionsAndMapsNumber(t *testing.T) {
	var prs []Item
	for index := 0; index < 10; index++ {
		prs = append(prs, Item{Kind: PR, PR: "1", Label: "PR", Command: "bgr 1"})
	}
	var output bytes.Buffer
	items := render(&output, Catalog{PRs: prs}, "")
	if len(items) != sectionLimit || !strings.Contains(output.String(), "2 more") {
		t.Fatalf("items %d, output %q", len(items), output.String())
	}
}

func TestRenderEscapesTerminalControlSequences(t *testing.T) {
	var output bytes.Buffer
	render(&output, Catalog{Commits: []Item{{Label: "subject\x1b]52;c;YQ==\a", Command: "bgr --commit abc"}}}, "")
	if strings.Contains(output.String(), "\x1b") || !strings.Contains(output.String(), `\u{1b}]52`) {
		t.Fatalf("unsafe picker output: %q", output.String())
	}
}

func TestRunSelectionAndQuit(t *testing.T) {
	commands := &pickerCommands{lookErr: errors.New("missing")}
	git := pickerGit{}
	var output bytes.Buffer
	selected, err := Run(context.Background(), Options{
		RepoDir: "/repo", Input: strings.NewReader("1\n"), Output: &output,
		Git: git, Commands: commands,
	})
	if err != nil || selected.Commit != "fullsha" || !strings.Contains(output.String(), "bgr --commit short") {
		t.Fatalf("selection %#v, err %v, output %q", selected, err, output.String())
	}
	_, err = Run(context.Background(), Options{
		RepoDir: "/repo", Input: strings.NewReader("q\n"), Output: &bytes.Buffer{},
		Git: git, Commands: commands,
	})
	if !errors.Is(err, ErrQuit) {
		t.Fatalf("quit err = %v", err)
	}
}

func TestDiscoverContinuesWithoutConventionalBase(t *testing.T) {
	catalog, err := Discover(context.Background(), "/repo", pickerNoBaseGit{}, &pickerCommands{lookErr: errors.New("missing")})
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Commits) != 1 || len(catalog.Branches) != 0 || !strings.Contains(strings.Join(catalog.Notes, "\n"), "Branches unavailable") {
		t.Fatalf("catalog = %#v", catalog)
	}
}

type pickerCommands struct{ lookErr error }

func (p *pickerCommands) LookPath(string) (string, error) { return "", p.lookErr }
func (p *pickerCommands) Run(context.Context, string, string, ...string) ([]byte, error) {
	return nil, errors.New("unused")
}

type pickerGit struct{}

func (pickerGit) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
	joined := strings.Join(args, " ")
	switch {
	case strings.Contains(joined, "symbolic-ref"):
		return nil, errors.New("none")
	case strings.Contains(joined, "rev-parse --verify --quiet origin/main"):
		return []byte("ok"), nil
	case strings.Contains(joined, "status --porcelain"):
		return nil, nil
	case strings.Contains(joined, "for-each-ref"):
		return nil, nil
	case strings.Contains(joined, "log -n 1000"):
		return []byte("fullsha\tshort\t1\tCommit subject\n"), nil
	default:
		return nil, errors.New("unexpected: " + joined)
	}
}

type pickerNoBaseGit struct{}

func (pickerNoBaseGit) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
	joined := strings.Join(args, " ")
	switch {
	case strings.Contains(joined, "status --porcelain"):
		return nil, nil
	case strings.Contains(joined, "log -n 1000"):
		return []byte("fullsha\tshort\t1\tCommit subject\n"), nil
	default:
		return nil, errors.New("no conventional base")
	}
}
