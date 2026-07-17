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
	Provider  string
	Model     string
	Reasoning string
}

func AnalysisPlan(calls int, provider, model, reasoning string) Plan {
	return Plan{
		Calls:     calls,
		Provider:  provider,
		Model:     model,
		Reasoning: reasoning,
	}
}

func Confirm(plan Plan, yes bool, input io.Reader, output io.Writer, inputIsTTY bool) error {
	return ConfirmWithThreshold(plan, CallThreshold, yes, input, output, inputIsTTY)
}

func ConfirmWithThreshold(plan Plan, threshold int, yes bool, input io.Reader, output io.Writer, inputIsTTY bool) error {
	if plan.Calls <= threshold {
		return nil
	}
	model := plan.Model
	if plan.Reasoning != "" {
		model += " (reasoning " + plan.Reasoning + ")"
	}
	fmt.Fprintf(output, "The fixed plan has exactly %d model calls on %s / %s; a failed stage may add its one allowed retry.\n",
		plan.Calls, plan.Provider, model)
	if yes {
		return nil
	}
	if !inputIsTTY {
		return fmt.Errorf("this run needs more than %d model calls - add --yes to approve the spend", threshold)
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
