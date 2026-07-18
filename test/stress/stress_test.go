package stress

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

const (
	stressEnv          = "BGR_STRESS"
	artifactBaseBytes  = int64(2 * 1_024 * 1_024)
	maxPeakRSSBytes    = int64(2 * 1_024 * 1_024 * 1_024)
	stressCommandLimit = 5 * time.Minute
)

type tierSpec struct {
	Name        string
	Description string
	TimeLimit   time.Duration
}

type tierResult struct {
	Kind        string
	Name        string
	RawBytes    int64
	HTMLBytes   int64
	Wall        time.Duration
	PeakRSS     int64
	RSSMeasured bool
	Status      string
}

var (
	stressEnabled  = os.Getenv(stressEnv) == "1"
	stressBinary   string
	stressTemp     string
	syntheticTable []tierResult
	historyTable   []tierResult
)

func TestMain(m *testing.M) {
	if stressEnabled {
		var err error
		stressTemp, err = os.MkdirTemp("", "bgr-stress-*")
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		name := "bgr"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		stressBinary = filepath.Join(stressTemp, name)
		command := exec.Command("go", "build", "-o", stressBinary, "../../cmd/bgr")
		command.Stdout = os.Stdout
		command.Stderr = os.Stderr
		if err := command.Run(); err != nil {
			_ = os.RemoveAll(stressTemp)
			os.Exit(1)
		}
	}
	status := m.Run()
	if stressEnabled {
		printTable("SYNTHETIC STRESS ACTUALS", syntheticTable)
		printTable("REAL-HISTORY STRESS ACTUALS", historyTable)
		_ = os.RemoveAll(stressTemp)
	}
	os.Exit(status)
}

func TestStressSyntheticTiers(t *testing.T) {
	requireStress(t)
	tiers := []tierSpec{
		// 20s, not 15: T1 ran at 13-14s on review hardware since v1.2 and
		// flaked under load at the v1.4 review — the owner-approved response
		// is a budget bump, never a weaker fixture.
		{Name: "T1", Description: "5,000 small files / 10 dirs", TimeLimit: 20 * time.Second},
		{Name: "T2", Description: "300 files x 2,000 lines", TimeLimit: 60 * time.Second},
		{Name: "T3", Description: "one 50MB file", TimeLimit: 60 * time.Second},
		{Name: "T4", Description: "one 1MB minified line", TimeLimit: 15 * time.Second},
		{Name: "T5", Description: "200 images among 300 files"},
	}
	syntheticTable = make([]tierResult, len(tiers))
	for index, tier := range tiers {
		index, tier := index, tier
		t.Run(tier.Name, func(t *testing.T) {
			patch := filepath.Join(stressTemp, strings.ToLower(tier.Name)+".patch")
			info, err := generateSynthetic(patch, tier.Name)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("%s: %s; generated %d files, %d changed lines, %d bytes",
				tier.Name, tier.Description, info.Files, info.Lines, info.Bytes)
			result := exerciseArtifact(t, "synthetic", tier.Name, patch, tier.TimeLimit)
			syntheticTable[index] = result
		})
	}
}

func TestStressRealHistory(t *testing.T) {
	requireStress(t)
	histories := []struct {
		Name       string
		Repository string
		Base       string
		Head       string
	}{
		{Name: "react-17-to-18", Repository: "https://github.com/facebook/react.git", Base: "v17.0.0", Head: "v18.0.0"},
		{Name: "typescript-4.9-to-5.0", Repository: "https://github.com/microsoft/TypeScript.git", Base: "v4.9.5", Head: "v5.0.2"},
		{Name: "linux-6.4-rc1-to-rc2", Repository: "https://github.com/torvalds/linux.git", Base: "v6.4-rc1", Head: "v6.4-rc2"},
	}
	cacheDir := os.Getenv("BGR_STRESS_CACHE")
	if cacheDir == "" {
		cacheDir = filepath.Join(os.TempDir(), "bgr-stress-cache")
	}
	historyTable = make([]tierResult, len(histories))
	for index, history := range histories {
		index, history := index, history
		t.Run(history.Name, func(t *testing.T) {
			command := exec.Command("sh", "fetch-history.sh",
				cacheDir, history.Name, history.Repository, history.Base, history.Head)
			var stdout, stderr bytes.Buffer
			command.Stdout = &stdout
			command.Stderr = &stderr
			if err := command.Run(); err != nil {
				reason := strings.TrimSpace(stderr.String())
				if reason == "" {
					reason = err.Error()
				}
				historyTable[index] = tierResult{
					Kind: "history", Name: history.Name, Status: "SKIP: " + oneLine(reason),
				}
				t.Logf("SKIP %s: shallow fetch failed: %s", history.Name, reason)
				return
			}
			patch := strings.TrimSpace(stdout.String())
			historyTable[index] = exerciseArtifact(t, "history", history.Name, patch, 0)
		})
	}
}

func exerciseArtifact(
	t *testing.T,
	kind, name, patch string,
	timeLimit time.Duration,
) tierResult {
	t.Helper()
	raw, err := os.Stat(patch)
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(stressTemp, name+".html")
	stateDir := filepath.Join(stressTemp, name+"-state")
	configDir := filepath.Join(stressTemp, name+"-config")
	ctx, cancel := context.WithTimeout(context.Background(), stressCommandLimit)
	defer cancel()
	command := exec.CommandContext(ctx, stressBinary,
		"--diff", patch,
		"--provider", "mock",
		"--no-cache",
		"--yes",
		"--out", output,
	)
	command.Env = append(os.Environ(),
		"XDG_STATE_HOME="+stateDir,
		"XDG_CONFIG_HOME="+configDir,
	)
	var stderr bytes.Buffer
	command.Stdout = io.Discard
	command.Stderr = &stderr
	started := time.Now()
	runErr := command.Run()
	wall := time.Since(started)
	peak, measured := peakRSSBytes(command.ProcessState)
	result := tierResult{
		Kind: kind, Name: name, RawBytes: raw.Size(), Wall: wall,
		PeakRSS: peak, RSSMeasured: measured, Status: "PASS",
	}
	if runErr != nil {
		result.Status = "FAIL: command"
		t.Errorf("%s failed after %s: %v\n%s", name, wall.Round(time.Millisecond), runErr, stderr.String())
		return result
	}
	stat, err := os.Stat(output)
	if err != nil {
		result.Status = "FAIL: missing artifact"
		t.Error(err)
		return result
	}
	result.HTMLBytes = stat.Size()
	if !completeHTML(output) {
		result.Status = "FAIL: truncated artifact"
		t.Errorf("%s artifact is incomplete or silently truncated", name)
	}
	if timeLimit > 0 && wall > timeLimit {
		result.Status = "FAIL: wall budget"
		t.Errorf("%s wall time %s exceeds %s", name, wall.Round(time.Millisecond), timeLimit)
	}
	ceiling := artifactBaseBytes + raw.Size()*5/2
	if stat.Size() > ceiling {
		result.Status = "FAIL: size budget"
		t.Errorf("%s artifact %d bytes exceeds ceiling %d", name, stat.Size(), ceiling)
	}
	if measured && peak > maxPeakRSSBytes {
		result.Status = "FAIL: RSS budget"
		t.Errorf("%s peak RSS %d exceeds %d", name, peak, maxPeakRSSBytes)
	}
	if !measured {
		t.Logf("%s: peak RSS unavailable on %s; assertion skipped", name, runtime.GOOS)
	}
	return result
}

func completeHTML(path string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil || stat.Size() == 0 {
		return false
	}
	size := min(stat.Size(), int64(4_096))
	buffer := make([]byte, size)
	if _, err := file.ReadAt(buffer, stat.Size()-size); err != nil && err != io.EOF {
		return false
	}
	return bytes.Contains(bytes.ToLower(buffer), []byte("</html>"))
}

func requireStress(t *testing.T) {
	t.Helper()
	if !stressEnabled {
		t.Skip("set BGR_STRESS=1 to run the opt-in monster stress suite")
	}
}

func printTable(title string, rows []tierResult) {
	fmt.Printf("\n%s\n", title)
	fmt.Println("| Tier | Raw | HTML | HTML/raw | Wall | Peak RSS | Status |")
	fmt.Println("|---|---:|---:|---:|---:|---:|---|")
	if len(rows) == 0 {
		fmt.Println("| - | - | - | - | - | - | NOT RUN |")
		return
	}
	for _, row := range rows {
		ratio := "-"
		if row.RawBytes > 0 {
			ratio = fmt.Sprintf("%.2fx", float64(row.HTMLBytes)/float64(row.RawBytes))
		}
		rss := "n/a"
		if row.RSSMeasured {
			rss = humanBytes(row.PeakRSS)
		}
		fmt.Printf("| %s | %s | %s | %s | %s | %s | %s |\n",
			row.Name, humanBytes(row.RawBytes), humanBytes(row.HTMLBytes), ratio,
			row.Wall.Round(time.Millisecond), rss, row.Status)
	}
}

func humanBytes(value int64) string {
	const unit = int64(1_024)
	if value < unit {
		return fmt.Sprintf("%d B", value)
	}
	if value < unit*unit {
		return fmt.Sprintf("%.1f KiB", float64(value)/float64(unit))
	}
	if value < unit*unit*unit {
		return fmt.Sprintf("%.1f MiB", float64(value)/float64(unit*unit))
	}
	return fmt.Sprintf("%.2f GiB", float64(value)/float64(unit*unit*unit))
}

func oneLine(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	if len(value) > 100 {
		return value[:100] + "..."
	}
	return value
}
