package provider

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestRunCommandTimeoutKillsHungProcess(t *testing.T) {
	if os.Getenv("BGR_COMMAND_SLEEP_HELPER") == "1" {
		time.Sleep(10 * time.Second)
		return
	}
	t.Setenv("BGR_COMMAND_SLEEP_HELPER", "1")
	start := time.Now()
	_, err := RunCommandTimeout(context.Background(), 50*time.Millisecond, "", nil, os.Args[0], "-test.run=TestRunCommandTimeoutKillsHungProcess")
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("error = %v, want timeout", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("timeout took %s", elapsed)
	}
}
