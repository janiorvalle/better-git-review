package provider

import (
	"strings"
	"testing"
)

func TestParseClaudeOutputShapes(t *testing.T) {
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
			got, err := ParseClaudeOutput([]byte(test.input))
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("got %q, want %q", got, test.want)
			}
		})
	}
}

func TestParseClaudeOutputErrorEvent(t *testing.T) {
	_, err := ParseClaudeOutput([]byte(`[{"type":"result","is_error":true,"result":"bad auth\u001b]52;c;YQ==\u0007"}]`))
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "\x1b") || !strings.Contains(err.Error(), `\x1b`) {
		t.Fatalf("provider control characters were not escaped: %q", err)
	}
}
