package guard

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

const CallThreshold = 5

type Plan struct {
	Calls     int
	MaxCalls  int
	Provider  string
	Model     string
	Reasoning string
}

func AnalysisPlan(fileCount int, staged bool, provider, model, reasoning string) Plan {
	calls := 1
	if staged {
		calls = fileCount + 1
	}
	return Plan{
		Calls:     calls,
		MaxCalls:  calls * 2,
		Provider:  provider,
		Model:     model,
		Reasoning: reasoning,
	}
}

func Confirm(plan Plan, yes bool, input io.Reader, output io.Writer, inputIsTTY bool) error {
	if plan.Calls <= CallThreshold {
		return nil
	}
	if plan.MaxCalls == 0 {
		plan.MaxCalls = plan.Calls * 2
	}
	model := plan.Model
	if plan.Reasoning != "" {
		model += " (reasoning " + plan.Reasoning + ")"
	}
	fmt.Fprintf(output, "This needs %d model calls on %s / %s - up to %d if retries kick in.\n",
		plan.Calls, plan.Provider, model, plan.MaxCalls)
	if yes {
		return nil
	}
	if !inputIsTTY {
		return fmt.Errorf("this run needs more than %d model calls - add --yes to approve the spend", CallThreshold)
	}
	fmt.Fprint(output, "Continue? [y/N] ")
	reader, ok := input.(*bufio.Reader)
	if !ok {
		reader = bufio.NewReader(input)
	}
	answer, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return err
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	if answer != "y" && answer != "yes" {
		return fmt.Errorf("cancelled - no model calls were made")
	}
	return nil
}
