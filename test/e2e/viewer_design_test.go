package e2e

import (
	"path/filepath"
	"strings"
	"testing"
)

// Gate M6 design lane (#12 ambient nav, #13 theme toggle): structural
// assertions on the rendered walkthrough. Kept in its own file so the main
// e2e suite can evolve independently.
func TestViewerNavAndThemeControls(t *testing.T) {
	env := isolatedEnvironment(t)
	output := filepath.Join(t.TempDir(), "design.html")
	result := runCLI(t, env, nil,
		"--diff", viewerFixturePath(t), "--provider", "mock", "--out", output)
	if result.err != nil {
		t.Fatalf("command failed: %v\n%s", result.err, result.stderr)
	}
	html, _ := readHTMLDocument(t, output)

	for _, expected := range []string{
		// #12: ambient step navigation in the sticky toolbar + keyboard hint.
		`id="tb-prev"`,
		`id="tb-next"`,
		`id="tb-count"`,
		`class="kbd-hint"`,
		// #13: three-state theme control and the pre-paint stamping script.
		`data-theme-target="auto"`,
		`data-theme-target="light"`,
		`data-theme-target="dark"`,
		`localStorage.getItem("bgr:theme")`,
		// #13: manual-override CSS scopes exist alongside the no-JS fallback.
		`:root[data-theme="dark"]`,
		`:root:not([data-theme])`,
		// Resizable sidebar rail (PR #9): handle, CSS variable, persistence.
		`id="rail-resizer"`,
		`var(--rail-width, 316px)`,
		`localStorage.getItem(railKey)`,
	} {
		if !strings.Contains(html, expected) {
			t.Fatalf("HTML missing %q", expected)
		}
	}

	// The slim pager must hide (not gray out) a missing direction, and the
	// theme stamper must run before the stylesheet so first paint is themed.
	if !strings.Contains(html, "pager-button:disabled { visibility: hidden; }") {
		t.Fatal("slim pager: disabled direction should be hidden")
	}
	if strings.Index(html, "bgr:theme") > strings.Index(html, "<style>") {
		t.Fatal("theme stamping script must precede the stylesheet")
	}
}
