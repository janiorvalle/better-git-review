package picker

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/janiorvalle/better-git-review/internal/gitexec"
	gitsource "github.com/janiorvalle/better-git-review/internal/source/git"
)

const sectionLimit = 8

var ErrQuit = errors.New("picker quit")

type Kind string

const (
	Dirty  Kind = "dirty"
	PR     Kind = "pr"
	Branch Kind = "branch"
	Commit Kind = "commit"
)

type Item struct {
	Kind    Kind
	Label   string
	Search  string
	PR      string
	Base    string
	Head    string
	Commit  string
	Command string
}

type Catalog struct {
	Dirty    []Item
	PRs      []Item
	Branches []Item
	Commits  []Item
	Notes    []string
}

type Selection struct {
	PR      string
	Base    string
	Head    string
	Commit  string
	Dirty   bool
	Command string
}

type CommandRunner interface {
	LookPath(string) (string, error)
	Run(context.Context, string, string, ...string) ([]byte, error)
}

type Options struct {
	RepoDir  string
	Query    string
	Input    io.Reader
	Output   io.Writer
	Git      gitexec.Runner
	Commands CommandRunner
}

func Run(ctx context.Context, opts Options) (Selection, error) {
	if opts.Input == nil {
		return Selection{}, fmt.Errorf("picker input is unavailable")
	}
	if opts.Output == nil {
		opts.Output = io.Discard
	}
	catalog, err := Discover(ctx, opts.RepoDir, opts.Git, opts.Commands)
	if err != nil {
		return Selection{}, err
	}
	reader, ok := opts.Input.(*bufio.Reader)
	if !ok {
		reader = bufio.NewReader(opts.Input)
	}
	query := strings.TrimSpace(opts.Query)
	for {
		visible := Filter(catalog, query)
		items := render(opts.Output, visible, query)
		fmt.Fprint(opts.Output, "Select a number, q to quit, or type to filter: ")
		answer, readErr := reader.ReadString('\n')
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return Selection{}, fmt.Errorf("read picker selection: %w", readErr)
		}
		answer = strings.TrimSpace(answer)
		if strings.EqualFold(answer, "q") {
			return Selection{}, ErrQuit
		}
		if number, numberErr := strconv.Atoi(answer); numberErr == nil && number > 0 && number <= len(items) {
			selected := items[number-1]
			fmt.Fprintf(opts.Output, "\u2192 running: %s\n", selected.Command)
			return mapSelection(selected), nil
		}
		query = answer
		if errors.Is(readErr, io.EOF) && answer == "" {
			return Selection{}, ErrQuit
		}
	}
}

func Discover(ctx context.Context, repoDir string, gitRunner gitexec.Runner, commands CommandRunner) (Catalog, error) {
	if gitRunner == nil {
		gitRunner = gitexec.ExecRunner{}
	}
	if commands == nil {
		commands = execRunner{}
	}
	base, err := gitsource.DetectBase(ctx, repoDir, gitRunner)
	result := Catalog{}
	if status, statusErr := gitRunner.Run(ctx, repoDir, "status", "--porcelain"); statusErr == nil && len(bytes.TrimSpace(status)) > 0 {
		result.Dirty = []Item{{
			Kind: Dirty, Label: "Uncommitted changes", Search: "dirty working tree uncommitted",
			Command: "bgr --dirty",
		}}
	}
	result.PRs, result.Notes = discoverPRs(ctx, repoDir, commands)
	if err == nil {
		result.Branches = discoverBranches(ctx, repoDir, base, gitRunner)
	} else {
		result.Notes = append(result.Notes, "Branches unavailable: pass --base to review a branch by ref.")
	}
	result.Commits = discoverCommits(ctx, repoDir, gitRunner)
	return result, nil
}

func Filter(catalog Catalog, query string) Catalog {
	result := Catalog{Notes: append([]string(nil), catalog.Notes...)}
	result.Dirty = filterItems(catalog.Dirty, query)
	result.PRs = filterItems(catalog.PRs, query)
	result.Branches = filterItems(catalog.Branches, query)
	result.Commits = filterItems(catalog.Commits, query)
	return result
}

func filterItems(items []Item, query string) []Item {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return append([]Item(nil), items...)
	}
	result := make([]Item, 0, len(items))
	for _, item := range items {
		if strings.Contains(strings.ToLower(item.Search+" "+item.Label), query) {
			result = append(result, item)
		}
	}
	return result
}

func render(output io.Writer, catalog Catalog, query string) []Item {
	fmt.Fprintln(output)
	if query == "" {
		fmt.Fprintln(output, "Review what?")
	} else {
		fmt.Fprintf(output, "Review what? (filter: %s)\n", safeTerminalText(query))
	}
	var flattened []Item
	sections := []struct {
		name  string
		items []Item
	}{
		{"WORKING TREE", catalog.Dirty},
		{"OPEN PULL REQUESTS", catalog.PRs},
		{"BRANCHES", catalog.Branches},
		{"RECENT COMMITS", catalog.Commits},
	}
	for _, section := range sections {
		if len(section.items) == 0 {
			continue
		}
		fmt.Fprintf(output, "\n%s\n", section.name)
		visible := section.items
		if len(visible) > sectionLimit {
			visible = visible[:sectionLimit]
		}
		for _, item := range visible {
			flattened = append(flattened, item)
			fmt.Fprintf(output, "  %2d  %s\n", len(flattened), safeTerminalText(item.Label))
		}
		if hidden := len(section.items) - len(visible); hidden > 0 {
			fmt.Fprintf(output, "      ... %d more - type to filter\n", hidden)
		}
	}
	for _, note := range catalog.Notes {
		fmt.Fprintf(output, "\n%s\n", safeTerminalText(note))
	}
	if len(flattened) == 0 {
		fmt.Fprintln(output, "\nNo matches.")
	}
	return flattened
}

func safeTerminalText(value string) string {
	var result strings.Builder
	for _, character := range value {
		if unicode.IsControl(character) {
			fmt.Fprintf(&result, "\\u{%x}", character)
			continue
		}
		result.WriteRune(character)
	}
	return result.String()
}

func mapSelection(item Item) Selection {
	return Selection{
		PR: item.PR, Base: item.Base, Head: item.Head, Commit: item.Commit,
		Dirty: item.Kind == Dirty, Command: item.Command,
	}
}

func discoverPRs(ctx context.Context, repoDir string, runner CommandRunner) ([]Item, []string) {
	if _, err := runner.LookPath("gh"); err != nil {
		return nil, []string{"GitHub PRs unavailable: install and authenticate gh to include them."}
	}
	if _, err := runner.Run(ctx, repoDir, "gh", "auth", "status", "--active"); err != nil {
		return nil, []string{"GitHub PRs unavailable: run `gh auth login` to include them."}
	}
	data, err := runner.Run(ctx, repoDir, "gh", "pr", "list", "--state", "open", "--limit", "1000", "--json", "number,title,updatedAt")
	if err != nil {
		return nil, []string{"GitHub PRs unavailable: `gh pr list` failed."}
	}
	var rows []struct {
		Number    int    `json:"number"`
		Title     string `json:"title"`
		UpdatedAt string `json:"updatedAt"`
	}
	if json.Unmarshal(data, &rows) != nil {
		return nil, []string{"GitHub PRs unavailable: could not read `gh pr list` output."}
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].UpdatedAt > rows[j].UpdatedAt })
	items := make([]Item, 0, len(rows))
	for _, row := range rows {
		number := strconv.Itoa(row.Number)
		items = append(items, Item{
			Kind: PR, PR: number, Label: fmt.Sprintf("#%d  %s", row.Number, row.Title),
			Search: number + " " + row.Title, Command: "bgr " + number,
		})
	}
	return items, nil
}

func discoverBranches(ctx context.Context, repoDir, base string, runner gitexec.Runner) []Item {
	data, err := runner.Run(ctx, repoDir, "for-each-ref", "--sort=-committerdate", "--format=%(refname:short)%09%(committerdate:iso8601)%09%(subject)", "refs/heads")
	if err != nil {
		return nil
	}
	var result []Item
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		fields := strings.SplitN(line, "\t", 3)
		if len(fields) == 0 || fields[0] == "" || fields[0] == strings.TrimPrefix(base, "origin/") {
			continue
		}
		countRaw, countErr := runner.Run(ctx, repoDir, "rev-list", "--count", base+".."+fields[0])
		count, _ := strconv.Atoi(strings.TrimSpace(string(countRaw)))
		if countErr != nil || count == 0 {
			continue
		}
		subject := ""
		if len(fields) == 3 {
			subject = fields[2]
		}
		result = append(result, Item{
			Kind: Branch, Base: base, Head: fields[0],
			Label:   fmt.Sprintf("%s  (%d ahead)  %s", fields[0], count, subject),
			Search:  fields[0] + " " + subject,
			Command: fmt.Sprintf("bgr --base %s --head %s", base, fields[0]),
		})
	}
	return result
}

func discoverCommits(ctx context.Context, repoDir string, runner gitexec.Runner) []Item {
	data, err := runner.Run(ctx, repoDir, "log", "-n", "1000", "--format=%H%x09%h%x09%ct%x09%s", "HEAD")
	if err != nil {
		return nil
	}
	var result []Item
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		fields := strings.SplitN(line, "\t", 4)
		if len(fields) != 4 {
			continue
		}
		result = append(result, Item{
			Kind: Commit, Commit: fields[0], Label: fields[1] + "  " + fields[3],
			Search: fields[0] + " " + fields[1] + " " + fields[3], Command: "bgr --commit " + fields[1],
		})
	}
	return result
}

type execRunner struct{}

func (execRunner) LookPath(name string) (string, error) { return exec.LookPath(name) }

func (execRunner) Run(ctx context.Context, cwd, name string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, name, args...)
	command.Dir = cwd
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = err.Error()
		}
		return nil, fmt.Errorf("%s", detail)
	}
	return stdout.Bytes(), nil
}
