package claude

import (
	"strings"
	"testing"

	"github.com/janiorvalle/better-git-review/internal/provider"
)

func TestUnsupportedEffortReturnsEffectiveDefault(t *testing.T) {
	selected, _, reasoning, warnings, err := newWithEffortSupport(provider.AdapterOptions{ReasoningOverride: "high"}, false)
	if err != nil {
		t.Fatal(err)
	}
	client := selected.(*CLI)
	if reasoning != "" || client.Reasoning != "" || len(warnings) != 1 {
		t.Fatalf("reasoning = %q, client = %q, warnings = %#v", reasoning, client.Reasoning, warnings)
	}
}

func TestParseOutputShapes(t *testing.T) {
	tests := map[string]struct {
		input string
		want  string
	}{
		"object": {
			input: `{"type":"result","is_error":false,"result":"{\"cohorts\":[]}"}`,
			want:  `{"cohorts":[]}`,
		},
		"event array": {
			input: `[{"type":"system"},{"type":"result","is_error":false,"result":"{\"cohorts\":[1]}"}]`,
			want:  `{"cohorts":[1]}`,
		},
		"bare string": {
			input: `"{\"cohorts\":[2]}"`,
			want:  `{"cohorts":[2]}`,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := ParseOutput([]byte(test.input))
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("got %q, want %q", got, test.want)
			}
		})
	}
}

func TestParseOutputErrorEvent(t *testing.T) {
	_, err := ParseOutput([]byte(`[{"type":"result","is_error":true,"result":"bad auth\u001b]52;c;YQ==\u0007"}]`))
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "\x1b") || !strings.Contains(err.Error(), `\x1b`) {
		t.Fatalf("provider control characters were not escaped: %q", err)
	}
}
