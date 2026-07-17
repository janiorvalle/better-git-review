package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func main() {
	distDir := flag.String("dist", "dist", "GoReleaser output directory")
	fixture := flag.String("fixture", "test/e2e/testdata/simple.patch", "diff fixture")
	flag.Parse()
	if err := run(*distDir, *fixture); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(distDir, fixture string) error {
	archives, err := archivePaths(distDir)
	if err != nil {
		return err
	}
	if len(archives) == 0 {
		return fmt.Errorf("no release archives found in %s", distDir)
	}
	var currentArchive string
	targetMarker := "_" + runtime.GOOS + "_" + runtime.GOARCH
	for _, archive := range archives {
		names, err := archiveNames(archive)
		if err != nil {
			return err
		}
		executableSuffix := ""
		if strings.HasSuffix(archive, ".zip") {
			executableSuffix = ".exe"
		}
		for _, expected := range []string{"bgr" + executableSuffix, "better-git-review" + executableSuffix} {
			if !containsBaseName(names, expected) {
				return fmt.Errorf("%s does not contain %s", archive, expected)
			}
		}
		if strings.Contains(filepath.Base(archive), targetMarker) {
			currentArchive = archive
		}
	}
	if currentArchive == "" {
		return fmt.Errorf("no archive found for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	fixture, err = filepath.Abs(fixture)
	if err != nil {
		return err
	}
	tempDir, err := os.MkdirTemp("", "bgr-artifact-smoke-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempDir)
	if err := extractArchive(currentArchive, tempDir); err != nil {
		return err
	}
	binaryName := "bgr"
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
	}
	binaryPath, err := findBaseName(tempDir, binaryName)
	if err != nil {
		return err
	}
	version := exec.Command(binaryPath, "--version")
	version.Dir = tempDir
	versionOutput, err := version.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s --version failed: %w\n%s", binaryName, err, versionOutput)
	}
	if !strings.HasPrefix(strings.TrimSpace(string(versionOutput)), "bgr ") {
		return fmt.Errorf("unexpected version output: %s", versionOutput)
	}
	outputPath := filepath.Join(tempDir, "walkthrough.html")
	walkthrough := exec.Command(
		binaryPath,
		"--diff", fixture,
		"--provider", "mock",
		"--out", outputPath,
	)
	walkthrough.Dir = tempDir
	walkthrough.Env = append(os.Environ(),
		"HOME="+tempDir,
		"APPDATA="+filepath.Join(tempDir, "appdata"),
		"LOCALAPPDATA="+filepath.Join(tempDir, "localappdata"),
		"XDG_CONFIG_HOME="+filepath.Join(tempDir, "config"),
		"XDG_STATE_HOME="+filepath.Join(tempDir, "state"),
	)
	if output, err := walkthrough.CombinedOutput(); err != nil {
		return fmt.Errorf("artifact walkthrough failed: %w\n%s", err, output)
	}
	html, err := os.ReadFile(outputPath)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(string(html))), "<!doctype html>") {
		return fmt.Errorf("artifact walkthrough did not produce HTML")
	}
	fmt.Printf("Artifact smoke passed for %d archives and %s/%s.\n", len(archives), runtime.GOOS, runtime.GOARCH)
	return nil
}

func archivePaths(distDir string) ([]string, error) {
	entries, err := os.ReadDir(distDir)
	if err != nil {
		return nil, err
	}
	var result []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || (!strings.HasSuffix(name, ".tar.gz") && !strings.HasSuffix(name, ".zip")) {
			continue
		}
		result = append(result, filepath.Join(distDir, name))
	}
	return result, nil
}

func archiveNames(path string) ([]string, error) {
	if strings.HasSuffix(path, ".zip") {
		reader, err := zip.OpenReader(path)
		if err != nil {
			return nil, err
		}
		defer reader.Close()
		names := make([]string, 0, len(reader.File))
		for _, file := range reader.File {
			names = append(names, file.Name)
		}
		return names, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return nil, err
	}
	defer gzipReader.Close()
	tarReader := tar.NewReader(gzipReader)
	var names []string
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		names = append(names, header.Name)
	}
	return names, nil
}

func extractArchive(path, destination string) error {
	if strings.HasSuffix(path, ".zip") {
		reader, err := zip.OpenReader(path)
		if err != nil {
			return err
		}
		defer reader.Close()
		for _, file := range reader.File {
			if file.FileInfo().IsDir() {
				continue
			}
			target, err := safeTarget(destination, file.Name)
			if err != nil {
				return err
			}
			input, err := file.Open()
			if err != nil {
				return err
			}
			if err := writeExtracted(target, file.Mode(), input); err != nil {
				input.Close()
				return err
			}
			input.Close()
		}
		return nil
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gzipReader.Close()
	tarReader := tar.NewReader(gzipReader)
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		target, err := safeTarget(destination, header.Name)
		if err != nil {
			return err
		}
		if err := writeExtracted(target, fs.FileMode(header.Mode), tarReader); err != nil {
			return err
		}
	}
}

func safeTarget(root, name string) (string, error) {
	target := filepath.Join(root, filepath.FromSlash(name))
	relative, err := filepath.Rel(root, target)
	if err != nil {
		return "", err
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("archive entry escapes extraction root: %s", name)
	}
	return target, nil
}

func writeExtracted(path string, mode fs.FileMode, input io.Reader) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	output, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode.Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(output, input); err != nil {
		output.Close()
		return err
	}
	return output.Close()
}

func containsBaseName(names []string, expected string) bool {
	for _, name := range names {
		if filepath.Base(filepath.FromSlash(name)) == expected {
			return true
		}
	}
	return false
}

func findBaseName(root, expected string) (string, error) {
	var result string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && entry.Name() == expected {
			result = path
			return fs.SkipAll
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if result == "" {
		return "", fmt.Errorf("extracted archive does not contain %s", expected)
	}
	return result, nil
}
