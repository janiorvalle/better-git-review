package provider

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

func RunCommand(ctx context.Context, dir string, input []byte, name string, args ...string) ([]byte, error) {
	return RunCommandTimeout(ctx, 0, dir, input, name, args...)
}

func RunCommandTimeout(ctx context.Context, timeout time.Duration, dir string, input []byte, name string, args ...string) ([]byte, error) {
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Stdin = bytes.NewReader(input)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, fmt.Errorf("%s: timed out after %s", name, timeout)
		}
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
