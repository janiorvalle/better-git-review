package policy

import (
	"bufio"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const modulePath = "github.com/janiorvalle/better-git-review"

func TestDependencyAllowlist(t *testing.T) {
	root := repoRoot(t)
	file, err := os.Open(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	allowed := map[string]bool{
		"github.com/BurntSushi/toml":      true,
		"github.com/alecthomas/chroma/v2": true,
		"github.com/dlclark/regexp2":      true,
	}
	found := map[string]bool{}
	inRequireBlock := false
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(strings.Split(scanner.Text(), "//")[0])
		switch {
		case line == "require (":
			inRequireBlock = true
			continue
		case inRequireBlock && line == ")":
			inRequireBlock = false
			continue
		case strings.HasPrefix(line, "require "):
			line = strings.TrimSpace(strings.TrimPrefix(line, "require "))
		case !inRequireBlock:
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		found[fields[0]] = true
		if !allowed[fields[0]] {
			t.Errorf("go.mod dependency %q is not in the allowlist", fields[0])
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	for dependency := range allowed {
		if !found[dependency] {
			t.Errorf("allowlisted dependency %q is missing from go.mod", dependency)
		}
	}
}

func TestInternalImportPolicy(t *testing.T) {
	root := repoRoot(t)
	internalRoot := filepath.Join(root, "internal")
	var violations []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if entry.Name() == ".git" || entry.Name() == "dist" || entry.Name() == "prototype" {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" {
			return nil
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, filepath.Dir(path))
		if err != nil {
			return err
		}
		packagePath := filepath.ToSlash(relative)
		for _, imported := range parsed.Imports {
			importPath, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				return err
			}
			if importPath == modulePath+"/internal/app" && packagePath != "cmd/bgr" &&
				packagePath != "cmd/better-git-review" {
				violations = append(violations, packagePath+" imports internal/app")
			}
			if !strings.HasPrefix(importPath, modulePath+"/internal/") {
				continue
			}
			if violation := checkInternalImport(packagePath, importPath); violation != "" {
				violations = append(violations, violation)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(internalRoot); err != nil {
		t.Fatal(err)
	}
	sort.Strings(violations)
	for _, violation := range violations {
		t.Error(violation)
	}
}

func checkInternalImport(packagePath, importPath string) string {
	importedInternal := strings.TrimPrefix(importPath, modulePath+"/internal/")
	switch packagePath {
	case "internal/document":
		return packagePath + " must not import internal/" + importedInternal
	case "internal/diff":
		return allowOnly(packagePath, importedInternal, "document")
	case "internal/viewer":
		return allowOnly(packagePath, importedInternal, "document")
	case "internal/cache":
		return allowOnly(packagePath, importedInternal, "document", "fileutil", "xdg")
	}
	if strings.HasPrefix(packagePath, "internal/provider/") {
		allowed := []string{"provider"}
		if packagePath == "internal/provider/mock" {
			allowed = append(allowed, "pathlayer")
		}
		return allowOnly(packagePath, importedInternal, allowed...)
	}
	if strings.HasPrefix(packagePath, "internal/source/") {
		allowed := []string{"source", "document"}
		if packagePath == "internal/source/git" {
			allowed = append(allowed, "gitexec")
		}
		return allowOnly(packagePath, importedInternal, allowed...)
	}
	return ""
}

func allowOnly(packagePath, imported string, allowed ...string) string {
	for _, candidate := range allowed {
		if imported == candidate {
			return ""
		}
	}
	return packagePath + " must not import internal/" + imported
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not locate policy test")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}
