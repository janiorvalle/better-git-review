package pathlayer

import "testing"

func TestClassify(t *testing.T) {
	tests := map[string]string{
		"db/migrations/001.sql":       "schema",
		"internal/service_test.go":    "tests",
		"api/routes/users.go":         "api",
		"frontend/components/App.tsx": "ui",
		".github/workflows/ci.yml":    "config",
		"docs/usage.md":               "docs",
		"internal/service.go":         "backend",
	}
	for path, want := range tests {
		if got := Classify(path); got != want {
			t.Fatalf("Classify(%q) = %q, want %q", path, got, want)
		}
	}
}
