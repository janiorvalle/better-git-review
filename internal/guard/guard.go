package guard

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

const CallThreshold = 5

type Plan struct {
	Calls    int
	MaxCalls int
	Provider string
	Model    string
}

func AnalysisPlan(fileCount int, staged bool, provider, model string) Plan {
	calls := 1
	if staged {
		calls = fileCount + 1
	}
	return Plan{
		Calls:    calls,
		MaxCalls: calls * 2,
		Provider: provider,
		Model:    model,
	}
}

func Confirm(plan Plan, yes bool, input io.Reader, output io.Writer, inputIsTTY bool) error {
	if plan.Calls <= CallThreshold {
		return nil
	}
	if plan.MaxCalls == 0 {
		plan.MaxCalls = plan.Calls * 2
	}
	fmt.Fprintf(output, "Analysis plan: %d calls using %q/%q (up to %d with validation retries)\n",
		plan.Calls, plan.Provider, plan.Model, plan.MaxCalls)
	if yes {
		return nil
	}
	if !inputIsTTY {
		return fmt.Errorf("analysis plan exceeds %d calls; rerun with --yes to approve it", CallThreshold)
	}
	fmt.Fprint(output, "Continue? [y/N] ")
	answer, err := bufio.NewReader(input).ReadString('\n')
	if err != nil && err != io.EOF {
		return err
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	if answer != "y" && answer != "yes" {
		return fmt.Errorf("analysis cancelled")
	}
	return nil
}
