package github

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/janiorvalle/better-git-review/internal/source"
)

func TestDiffRetriesServerErrors(t *testing.T) {
	runner := &fakeRunner{responses: []fakeResponse{
		{err: errors.New("HTTP 503 Service Unavailable")},
		{err: errors.New("HTTP 502 Bad Gateway")},
		{data: []byte("diff")},
	}}
	var sleeps int
	got, err := (Source{
		RetryBackoff: time.Nanosecond,
		Sleep:        func(context.Context, time.Duration) error { sleeps++; return nil },
	}).diffWithRetry(context.Background(), runner, source.Options{PR: "7", RepoDir: "/repo"})
	if err != nil || string(got) != "diff" || sleeps != 2 {
		t.Fatalf("got %q, sleeps %d, err %v", got, sleeps, err)
	}
}

func TestDiffDoesNotRetryClientErrors(t *testing.T) {
	runner := &fakeRunner{responses: []fakeResponse{{err: errors.New("HTTP 404 Not Found")}}}
	var sleeps int
	_, err := (Source{Sleep: func(context.Context, time.Duration) error { sleeps++; return nil }}).
		diffWithRetry(context.Background(), runner, source.Options{PR: "7", RepoDir: "/repo"})
	if err == nil || sleeps != 0 || len(runner.calls) != 1 {
		t.Fatalf("err %v, sleeps %d, calls %#v", err, sleeps, runner.calls)
	}
}

func TestDetectExplainsMissingAndUnauthenticatedGH(t *testing.T) {
	missing := Source{Runner: &fakeRunner{lookErr: errors.New("missing")}}
	available, detail := missing.Detect(source.Options{PR: "5"})
	if available || !strings.Contains(detail, "cli.github.com") || !strings.Contains(detail, "--base/--head") {
		t.Fatalf("missing detail = %q", detail)
	}
	unauth := Source{Runner: &fakeRunner{responses: []fakeResponse{{err: errors.New("not logged in")}}}}
	available, detail = unauth.Detect(source.Options{PR: "5"})
	if available || !strings.Contains(detail, "gh auth login") || !strings.Contains(detail, "--commit") {
		t.Fatalf("unauth detail = %q", detail)
	}
}

type fakeResponse struct {
	data []byte
	err  error
}

type fakeRunner struct {
	lookErr   error
	responses []fakeResponse
	calls     []string
}

func (f *fakeRunner) LookPath(string) (string, error) {
	return "/bin/gh", f.lookErr
}

func (f *fakeRunner) Run(_ context.Context, _ string, name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, strings.Join(append([]string{name}, args...), " "))
	if len(f.responses) == 0 {
		return nil, fmt.Errorf("unexpected call")
	}
	response := f.responses[0]
	f.responses = f.responses[1:]
	return response.data, response.err
}
