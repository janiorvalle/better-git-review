package app

import (
	"bytes"
	"testing"
)

func TestParseArgsAllowsPRBeforeFlags(t *testing.T) {
	opts, err := parseArgs([]string{"123", "--provider", "mock", "--out", "result.json"}, Environment{
		Stderr: &bytes.Buffer{},
		Getwd:  func() (string, error) { return t.TempDir(), nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if opts.PR != "123" || opts.Provider != "mock" || opts.Out != "result.json" {
		t.Fatalf("unexpected options: %#v", opts)
	}
}
