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
	Provider string
	Model    string
}

func Confirm(plan Plan, yes bool, input io.Reader, output io.Writer, inputIsTTY bool) error {
	if plan.Calls <= CallThreshold {
		return nil
	}
	fmt.Fprintf(output, "Analysis plan: %d calls using %s/%s\n", plan.Calls, plan.Provider, plan.Model)
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
