package guard

import (
	"bytes"
	"strings"
	"testing"
)

func TestConfirmCostGuard(t *testing.T) {
	var output bytes.Buffer
	if err := Confirm(Plan{Calls: 5}, false, strings.NewReader(""), &output, false); err != nil {
		t.Fatalf("threshold should not prompt: %v", err)
	}
	if err := Confirm(Plan{Calls: 6, Provider: "mock", Model: "test"}, false,
		strings.NewReader(""), &output, false); err == nil {
		t.Fatal("non-TTY oversized plan should be refused")
	}
	if err := Confirm(Plan{Calls: 6, Provider: "mock", Model: "test"}, true,
		strings.NewReader(""), &output, false); err != nil {
		t.Fatalf("--yes should approve: %v", err)
	}
	if err := Confirm(Plan{Calls: 6}, false, strings.NewReader("yes\n"), &output, true); err != nil {
		t.Fatalf("interactive yes should approve: %v", err)
	}
}

func TestAnalysisPlanArithmetic(t *testing.T) {
	single := AnalysisPlan(50, false, "mock", "test")
	if single.Calls != 1 || single.MaxCalls != 2 {
		t.Fatalf("single-pass plan = %#v", single)
	}
	staged := AnalysisPlan(6, true, "mock", "test")
	if staged.Calls != 7 || staged.MaxCalls != 14 {
		t.Fatalf("staged plan = %#v", staged)
	}
}
