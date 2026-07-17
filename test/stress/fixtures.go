package stress

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
)

const (
	t2FileCount     = 300
	t2LinesPerFile  = 2_000
	t3LineCount     = 50 * 1_024
	t4LineTextBytes = 1 * 1_024 * 1_024
)

type fixtureInfo struct {
	Files int
	Lines int
	Bytes int64
}

func generateSynthetic(path, tier string) (fixtureInfo, error) {
	file, err := os.Create(path)
	if err != nil {
		return fixtureInfo{}, err
	}
	writer := bufio.NewWriterSize(file, 1<<20)
	var info fixtureInfo
	switch tier {
	case "T1":
		info, err = writeT1(writer)
	case "T2":
		info, err = writeT2(writer)
	case "T3":
		info, err = writeT3(writer)
	case "T4":
		info, err = writeT4(writer)
	case "T5":
		info, err = writeT5(writer)
	default:
		err = fmt.Errorf("unknown synthetic tier %q", tier)
	}
	if flushErr := writer.Flush(); err == nil {
		err = flushErr
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fixtureInfo{}, err
	}
	stat, err := os.Stat(path)
	if err != nil {
		return fixtureInfo{}, err
	}
	info.Bytes = stat.Size()
	return info, nil
}

func writeT1(output io.Writer) (fixtureInfo, error) {
	const files = 5_000
	for index := 0; index < files; index++ {
		directory := index % 10
		if _, err := fmt.Fprintf(output, `diff --git a/dir-%02d/file-%04d.go b/dir-%02d/file-%04d.go
new file mode 100644
index 0000000..1111111
--- /dev/null
+++ b/dir-%02d/file-%04d.go
@@ -0,0 +1,3 @@
+package dir%d
+const FileID = %d
+func Value() int { return FileID }
`, directory, index, directory, index, directory, index, directory, index); err != nil {
			return fixtureInfo{}, err
		}
	}
	return fixtureInfo{Files: files, Lines: files * 3}, nil
}

func writeT2(output io.Writer) (fixtureInfo, error) {
	for fileIndex := 0; fileIndex < t2FileCount; fileIndex++ {
		if _, err := fmt.Fprintf(output, `diff --git a/src/file-%03d.go b/src/file-%03d.go
new file mode 100644
index 0000000..1111111
--- /dev/null
+++ b/src/file-%03d.go
@@ -0,0 +1,%d @@
`, fileIndex, fileIndex, fileIndex, t2LinesPerFile); err != nil {
			return fixtureInfo{}, err
		}
		for line := 1; line <= t2LinesPerFile; line++ {
			if _, err := fmt.Fprintf(output,
				"+const value%04d = %d // synthetic stress line\n", line, line); err != nil {
				return fixtureInfo{}, err
			}
		}
	}
	return fixtureInfo{Files: t2FileCount, Lines: t2FileCount * t2LinesPerFile}, nil
}

func writeT3(output io.Writer) (fixtureInfo, error) {
	if _, err := fmt.Fprintf(output, `diff --git a/large.txt b/large.txt
new file mode 100644
index 0000000..1111111
--- /dev/null
+++ b/large.txt
@@ -0,0 +1,%d @@
`, t3LineCount); err != nil {
		return fixtureInfo{}, err
	}
	line := "+" + strings.Repeat("x", 1_022) + "\n"
	for range t3LineCount {
		if _, err := io.WriteString(output, line); err != nil {
			return fixtureInfo{}, err
		}
	}
	return fixtureInfo{Files: 1, Lines: t3LineCount}, nil
}

func writeT4(output io.Writer) (fixtureInfo, error) {
	if _, err := io.WriteString(output, `diff --git a/dist/app.min.js b/dist/app.min.js
new file mode 100644
index 0000000..1111111
--- /dev/null
+++ b/dist/app.min.js
@@ -0,0 +1 @@
+`); err != nil {
		return fixtureInfo{}, err
	}
	if _, err := io.WriteString(output, strings.Repeat("x", t4LineTextBytes)); err != nil {
		return fixtureInfo{}, err
	}
	if _, err := io.WriteString(output, "\n"); err != nil {
		return fixtureInfo{}, err
	}
	return fixtureInfo{Files: 1, Lines: 1}, nil
}

func writeT5(output io.Writer) (fixtureInfo, error) {
	const imageFiles = 200
	const textFiles = 100
	for index := 0; index < imageFiles; index++ {
		if _, err := fmt.Fprintf(output, `diff --git a/assets/image-%03d.png b/assets/image-%03d.png
new file mode 100644
index 0000000..1111111
Binary files /dev/null and b/assets/image-%03d.png differ
`, index, index, index); err != nil {
			return fixtureInfo{}, err
		}
	}
	for index := 0; index < textFiles; index++ {
		if _, err := fmt.Fprintf(output, `diff --git a/src/helper-%03d.go b/src/helper-%03d.go
new file mode 100644
index 0000000..1111111
--- /dev/null
+++ b/src/helper-%03d.go
@@ -0,0 +1 @@
+package helpers
`, index, index, index); err != nil {
			return fixtureInfo{}, err
		}
	}
	return fixtureInfo{Files: imageFiles + textFiles, Lines: textFiles}, nil
}
