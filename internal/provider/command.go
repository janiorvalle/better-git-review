package provider

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

func RunCommand(ctx context.Context, dir string, input []byte, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Stdin = bytes.NewReader(input)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = err.Error()
		}
		return nil, fmt.Errorf("%s: %s", name, SafeDiagnostic(detail, 1_000))
	}
	return stdout.Bytes(), nil
}

func SafeDiagnostic(value string, limit int) string {
	if len(value) > limit {
		value = value[:limit] + "... [truncated]"
	}
	return strconv.QuoteToASCII(value)
}
