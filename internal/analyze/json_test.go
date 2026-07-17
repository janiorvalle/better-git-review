package analyze

import (
	"encoding/json"
	"testing"
)

func TestExtractJSONShapes(t *testing.T) {
	tests := map[string]string{
		"bare":   `{"title":"ok"}`,
		"fenced": "```json\n{\"title\":\"ok\"}\n```",
		"prose":  `Here is the result: {"title":"ok"} Thanks.`,
	}
	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			value, err := ExtractJSON(input)
			if err != nil {
				t.Fatal(err)
			}
			if !json.Valid(value) {
				t.Fatalf("not valid JSON: %s", value)
			}
		})
	}
}

func TestExtractJSONMissingObject(t *testing.T) {
	if _, err := ExtractJSON("not json"); err == nil {
		t.Fatal("expected an error")
	}
}

func TestRepairJSON(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "unescaped quote",
			input: `{"narrative":"calls "Save" before returning","files":[0]}`,
			want:  `calls "Save" before returning`,
		},
		{
			name:  "raw newline",
			input: "{\"narrative\":\"first line\nsecond line\",\"files\":[0]}",
			want:  "first line\nsecond line",
		},
		{
			name:  "raw tab and carriage return",
			input: "{\"narrative\":\"first\tsecond\rthird\",\"files\":[0]}",
			want:  "first\tsecondthird",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var decoded struct {
				Narrative string `json:"narrative"`
			}
			if err := json.Unmarshal([]byte(RepairJSON(test.input)), &decoded); err != nil {
				t.Fatal(err)
			}
			if decoded.Narrative != test.want {
				t.Fatalf("got %q, want %q", decoded.Narrative, test.want)
			}
		})
	}
}
