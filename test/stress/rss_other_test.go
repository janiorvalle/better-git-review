//go:build !darwin && !linux

package stress

import "os"

func peakRSSBytes(_ *os.ProcessState) (int64, bool) {
	return 0, false
}
