package configure

import (
	"bufio"
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestPromptRequiresARealEmptyLineForDefault(t *testing.T) {
	if _, err := prompt(bufio.NewReader(strings.NewReader("")), &bytes.Buffer{}, "Model", "default"); !errors.Is(err, ErrCancelled) {
		t.Fatalf("EOF error = %v", err)
	}
	value, err := prompt(bufio.NewReader(strings.NewReader("\n")), &bytes.Buffer{}, "Model", "default")
	if err != nil || value != "default" {
		t.Fatalf("value = %q, error = %v", value, err)
	}
}

func TestYesNoRequiresARealEmptyLineForDefault(t *testing.T) {
	if _, err := promptYesNo(bufio.NewReader(strings.NewReader("")), &bytes.Buffer{}, "Continue?", true); !errors.Is(err, ErrCancelled) {
		t.Fatalf("EOF error = %v", err)
	}
	value, err := promptYesNo(bufio.NewReader(strings.NewReader("\n")), &bytes.Buffer{}, "Continue?", true)
	if err != nil || !value {
		t.Fatalf("value = %v, error = %v", value, err)
	}
}
