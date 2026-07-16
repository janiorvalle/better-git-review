# Gate M2 — Viewer (handoff)

You are implementing Gate M2 of `better-git-review`. Read `PLAN.md` at the
repo root first — it is the design authority. Gate M1 (merged) built the core
pipeline that produces a schema-versioned walkthrough JSON document. **Your
gate renders that document as the product: a single self-contained HTML
guided walkthrough with GitHub-fidelity diffs.**

Reference material in this repo:
- `prototype/template.html` — working proof of the walkthrough UX (step nav,
  overview, collapsible diffs). Treat as UX reference, not as code to port
  verbatim; the M2 bar is far higher on diff rendering.
- `reference/coderabbit-diff-viewer.png` — visual reference for cohort
  sidebar, split view, folded unmodified regions, per-hunk attribution.

## Precondition

`main` must contain the merged Gate M1. Branch `gate/m2-viewer` off `main`.
PR to `main` when done; leave unmerged. Do not modify `PLAN.md`,
`prototype/`, `reference/`, or `handoffs/`. Document all deviations in the
PR under "Deviations & decisions". Commit incrementally.

## Deliverables

### 1. CLI changes

- Default output becomes **HTML**: `walkthrough-<name>.html`.
- `--format html|json` (default `html`) — `json` preserves the exact M1
  document output for tooling.
- `--open` — after writing, open the HTML in the default browser
  (`open` on darwin, `xdg-open` on linux; silently skip elsewhere/on error).
  Never the default.
- Everything else from M1 (sources, providers, cache, trust, guard) is
  untouched.

### 2. Document schema additions (bump `schemaVersion` to 2)

- `files[].hunks[].blame` (optional): `{ "author": "...", "date": "ISO8601" }`
  — most recent commit touching that hunk's new-side lines. Populate ONLY for
  local-git sources (`source.repoDir` non-empty); omit for patch mode, and
  skip silently for any file/hunk where `git blame` fails (renames, deleted
  files, huge files). Use `git blame --porcelain -L <start>,<end> HEAD --
  <path>` per hunk; if per-hunk cost is a problem on large diffs, one blame
  per file and slicing is an acceptable optimization. Blame must never fail
  the run and must respect the M1 hardening posture (no textconv, no color).
- The schemaVersion bump naturally invalidates M1 cache entries (key already
  includes schemaVersion) — verify, don't reimplement.

### 3. The viewer (embedded in the binary via `embed`)

A single self-contained HTML file: inline CSS + JS, document JSON embedded in
a `<script type="application/json">` island (escape `</script` sequences).
No external requests EXCEPT the one optional mermaid CDN script (see §5).
Works from `file://`, offline (minus the diagram), in current Chrome/Safari/
Firefox.

Walkthrough structure (locked, PLAN #9 — prototype shows the shape):

- **Sidebar**: overview entry + numbered cohort steps, layer badge and file
  count per step; current step highlighted; click to jump.
- **Overview step**: analysis title, overview text, total files/+/− counts,
  relationship diagram (§5), clickable cohort table of contents.
- **Cohort steps**: title, layer badge, intent line, narrative block, review
  notes ("worth double-checking") when present, **dependency chips** —
  "Depends on: <cohort title>" linking to the referenced earlier cohort
  (PLAN #14) — then the cohort's file diffs.
- **Navigation**: prev/next pager with step names, ←/→ keyboard navigation,
  step counter ("Step 2 of 7").
- **Per-hunk blame strip** (when blame data exists): author + date above the
  hunk, GitHub-subtle styling.
- Dark/light via `prefers-color-scheme`, both fully styled.

### 4. GitHub-fidelity diff rendering (locked, PLAN #2 and #10)

This is the heart of the gate. Match GitHub's "Files changed" view:

- **Primer tokens** — light: addition bg `#e6ffec`, addition word-accent
  `#abf2bc`, deletion bg `#ffebe9`, deletion word-accent `#ffcecb`, border
  `#d1d9e0`, canvas `#ffffff`. Dark: canvas `#0d1117`, addition
  `rgba(46,160,67,0.15)` with word-accent `rgba(46,160,67,0.4)`, deletion
  `rgba(248,81,73,0.1)` with word-accent `rgba(248,81,73,0.4)`, text
  `#f0f6fc`. Line-number gutter, hunk-header row styling per GitHub.
- **Fonts/metrics** — code: `ui-monospace, SFMono-Regular, "SF Mono", Menlo,
  Consolas, "Liberation Mono", monospace` at 12px/20px; UI: GitHub's
  system-font stack.
- **Syntax highlighting** — server-side at generation time via
  `github.com/alecthomas/chroma/v2` (now an allowed dependency), `github` and
  `github-dark` styles, lexer chosen from file extension with plain-text
  fallback. Highlight per line so add/del/context row styling composes with
  token colors. CSS classes, not inline styles, to keep file size sane.
- **Unified AND split (side-by-side) views** with a per-walkthrough toggle
  (persist choice in `localStorage`). Split view aligns old/new line pairs
  with hatched/empty placeholder on the missing side (see the CodeRabbit
  reference).
- **Word-level intra-line highlights** — for paired del/add lines, compute
  the changed spans and mark them with the word-accent colors. Pair lines the
  way GitHub does (contiguous del-block followed by add-block of equal
  count pairs 1:1; unequal blocks pair the overlap). A common-prefix/suffix
  trim per pair is the acceptable minimum; token-level LCS is better. Do
  this at generation time in Go, emit `<span>` marks. Unit-test the
  algorithm hard (empty, identical, fully-different, multi-byte/UTF-8,
  whitespace-only changes).
- **Unmodified-region folding** — consecutive context lines beyond ~10
  collapse into a "N unmodified lines" bar that expands in place on click
  (content present in the HTML, hidden — no lazy loading needed).
- **File chrome** — per-file header (path, status tag, +/− counts), sticky
  within its file while scrolling, collapsible; files over ~400 changed
  lines start collapsed (prototype behavior).

### 5. Diagram

`analysis.mermaid` renders via mermaid loaded from the CDN **with graceful
degradation**: offline or CSP-blocked, show the diagram source in a styled
`<pre>` instead. This is the single allowed external request, isolated so
its failure affects nothing else. (Accepted tradeoff, recorded here per
PLAN's open "diagram renderer" detail.)

### 6. Security requirements (gate-blocking)

Diff content, file paths, commit metadata, and model-generated text are ALL
untrusted and land in HTML:

- Every interpolated value is escaped (Go `html/template` contextual
  escaping or equivalent rigor for JS-side rendering). The JSON island
  escapes `<` (`<`).
- A hostile fixture diff containing `<script>alert(1)</script>`,
  `"></div><img src=x onerror=...>`, and a `</script>` sequence inside diff
  text MUST render inert — required unit/e2e coverage.
- No inline event handlers built from data; no `innerHTML` with unescaped
  data in the viewer JS.
- Blame/author strings are escaped like everything else.

## How to test/validate (per PLAN.md testing policy)

### Unit tests

- Word-diff pairing + span computation (cases listed in §4).
- Folding boundaries (exactly at threshold, nested between hunks).
- Blame porcelain parsing; blame skip-on-failure.
- HTML escaping: hostile strings in every data slot (paths, hunk headers,
  narratives, blame authors) produce escaped output.
- Chroma integration: known extension → highlighted spans; unknown → plain.

### E2E tests (extend `test/e2e`)

1. **HTML happy path**: mock provider + fixture patch → exit 0; output file
   parses as HTML; contains the JSON island, one section per cohort, sidebar
   entries, both unified and split view markup, folded-region bars.
2. **Self-containment**: the only external reference in the HTML is the
   mermaid CDN URL — assert via allowlist scan of `src=`/`href=`/`url(`.
3. **XSS fixture**: hostile patch (see §6) → no unescaped `<script>` or
   `onerror` from data reaches the HTML (assert on raw bytes).
4. **`--format json`**: byte-for-byte M1-shaped document (plus schema v2
   additions), still validates.
5. **Blame**: throwaway git repo with two authors/commits → hunks carry
   correct blame; patch-mode run carries none.
6. **Cache schema bump**: a v1-keyed cache entry is not reused (fresh
   analysis happens; key differs).

### Real-provider e2e (opt-in, unchanged gating)

`BGR_E2E_CLAUDE=1`: real diff → HTML renders, document validates.

### Validation commands

```sh
go build ./... && go vet ./... && go test ./...
BGR_E2E_CLAUDE=1 go test ./test/e2e/ -run Claude -v -count=1
```

Also include in the PR: one real generated walkthrough HTML (any small real
diff) attached or a path/screenshot — the reviewer will open it in a browser
and judge fidelity against GitHub and the CodeRabbit reference.

## Out of scope — do NOT build

Staged analysis for huge diffs (M3), goreleaser/releases (M3), GitLab
adapters, `--serve` mode, "viewed" checkboxes, semantic delta labels,
within-cohort file stepper, streaming.

## Dependency policy (updated for M2)

`BurntSushi/toml`, `alecthomas/chroma/v2` (+ its transitive deps). Nothing
else. Viewer JS/CSS is hand-written and embedded — no frontend frameworks,
no npm, no bundler.

## Acceptance checklist (the review will check exactly this)

- [ ] Build/vet/test clean from a clean checkout, no network
- [ ] go.mod: only toml + chroma (+ transitives)
- [ ] Schema v2: blame additions, M1 cache naturally invalidated
- [ ] All §4 fidelity features present: Primer tokens both themes, chroma
      highlighting, unified + split with toggle + localStorage, word-level
      marks, folding, sticky file headers, collapse-large-files
- [ ] Walkthrough structure complete: sidebar, overview + diagram + TOC,
      cohort steps with narratives/notes/dependency chips, keyboard nav,
      blame strips on local-git runs
- [ ] Security: XSS fixtures inert (unit + e2e), JSON island escaped,
      self-containment allowlist test passes
- [ ] All six e2e scenarios above present and passing; opt-in Claude e2e
      passes when gated
- [ ] Real generated HTML provided for visual review
- [ ] PR from `gate/m2-viewer`, unmerged, with "Deviations & decisions"
