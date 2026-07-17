//go:build darwin || linux

package stress

import (
	"os"
	"runtime"
	"syscall"
)

func peakRSSBytes(state *os.ProcessState) (int64, bool) {
	if state == nil {
		return 0, false
	}
	usage, ok := state.SysUsage().(*syscall.Rusage)
	if !ok || usage.Maxrss <= 0 {
		return 0, false
	}
	value := int64(usage.Maxrss)
	if runtime.GOOS == "linux" {
		value *= 1_024
	}
	return value, true
}
