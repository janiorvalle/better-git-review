# PLAN M4 — Repo Hardening, Platform Polish, Simplification

**Status: draft for review** — becomes `handoffs/GATE-M4.md` once approved.
Source of lessons: janiorvalle/drawover (npm OSS repo with mature CI/release
hygiene). This plan adapts what transfers to a Go binary project, adds the
platform items from dogfooding feedback, and folds in a simplification pass
grounded in observations from the M1–M3 gate reviews.

---

## Part 1 — Lessons from drawover

### Adopt (transfers directly)

1. **Least-privilege workflow permissions.** Top-level `permissions: {}` on
   every workflow; each job grants only what it needs, with a comment
   explaining why (drawover annotates every grant). Our current ci/release
   workflows use defaults — tighten them.
2. **SHA-pinned actions.** Every `uses:` pinned to a full commit SHA with a
   `# vX.Y.Z` comment. Supply-chain 101; we currently pin to tags.
3. **`persist-credentials: false`** on every checkout that doesn't push.
4. **Concurrency groups.** CI: `cancel-in-progress: true` keyed on PR;
   release: `cancel-in-progress: false` (never kill a half-done release).
5. **`timeout-minutes` on every job.** Runaway jobs burn minutes silently.
6. **Fork guard on release.** `if: github.repository == 'janiorvalle/better-git-review'`
   so a fork with a copied tag can never attempt a publish.
7. **Protected `environment: release`.** The release job runs in a GitHub
   environment with protection rules — publishing requires the environment's
   approval gate even if someone can push tags.
8. **Build provenance.** drawover uses npm trusted publishing (OIDC,
   `--provenance`). Go equivalent: `actions/attest-build-provenance` on the
   goreleaser artifacts (native GitHub attestation), keeping checksums.txt.
9. **Secret scanning: gitleaks.** `.gitleaks.toml` + pre-commit hook +
   a CI job running the same scan. Cheap insurance for a repo destined
   to go public — catches a pasted OPENROUTER key before it lands.
10. **SECURITY.md.** Disclosure policy; required for a serious public repo
    (and Scorecard checks it).
11. **OpenSSF Scorecard workflow.** Runs on public repos; harmless until the
    flip. Add it dormant, get the grade for free on day one public.
12. **Docs-only path filtering in CI.** Markdown-only PRs skip the build/test
    jobs (drawover uses dorny/paths-filter). Our suite is fast, but the
    pattern costs three lines and keeps doc PRs friction-free.
13. **Release artifact smoke test.** drawover's `verify-pack` unpacks the
    artifact and exercises it. Ours: after goreleaser snapshot in CI, run the
    built binary from the archive (`--version`, one `--diff fixture
    --provider mock` run) — proves the shipped artifact works, not just the
    source tree.

### Adapt (right idea, different mechanics for a Go binary)

14. **The `verify` meta-command.** drawover has one `pnpm verify` that runs
    the entire local gate. Ours is scattered across handoff docs. Add a
    `Makefile` with `make verify` = build + vet + test + goreleaser check +
    artifact smoke. One command for contributors and review alike; CI calls
    the same target so local and CI can't drift.
15. **Policy-as-test.** drawover has verify-architecture/dependencies/infra
    scripts enforcing structural rules. Ours live only in handoff prose
    (dependency allowlist, layering rules). Codify as a normal Go test:
    parse go.mod and assert the allowlist (toml, chroma + transitives);
    assert forbidden imports (e.g. nothing outside `internal/provider`
    imports `net/http`; see Part 3 layering). Policies enforce themselves
    from then on.
16. **Version-PR / changelog flow.** drawover uses changesets: pending
    change notes accumulate, a bot PR bumps the version and writes the
    changelog, and the tag publishes. A binary with tag-derived versioning
    doesn't need version-bump PRs — but the changelog half transfers:
    goreleaser's changelog generation is already configured; adopt
    conventional-ish commit prefixes (`feat:`/`fix:`/`docs:`) so the
    generated release notes group meaningfully. Skip the bot.

### Skip (doesn't fit)

- ~~CLA workflow~~ — **moved to adopt by owner decision** (see locked
  answers): port drawover's CLA workflow.
- **size-limit budgets** — meaningful for a browser lib; a CLI binary's size
  is not a product constraint. The artifact smoke test covers "did we ship
  something sane."
- **changesets bot itself** — see #16.

---

## Part 2 — Platform polish (from dogfooding feedback)

17. **`bgr` as the PRIMARY name** *(locked by owner)*. The binary is `bgr`
    (`cmd/bgr`); release archives also ship `better-git-review` as the long
    alias. README, --help, docs, and examples lead with `bgr` everywhere;
    the repo name stays `better-git-review` (discoverability).
18. **First-class Windows.**
    - `--open`: use `cmd /c start` (or `rundll32 url.dll`) on Windows
      instead of silently no-opping.
    - Config/state/cache paths: honor `%APPDATA%`/`%LOCALAPPDATA%` via
      `os.UserConfigDir()`/`os.UserCacheDir()` on Windows while keeping
      current XDG behavior on unix (note: this MOVES Windows paths — no
      existing Windows users yet, so no migration needed; unix paths must
      not change).
    - CI: add a `windows-latest` job (build + unit tests at minimum; e2e
      subprocess tests if the fixtures are path-safe — make them).
    - One real manual smoke on a Windows machine before calling it done,
      documented in the PR.
19. **`--dirty` flag.** Explicitly review only uncommitted changes
    (`git diff HEAD`) even when the branch also has commits vs. base —
    today's automatic fallback only triggers when the committed diff is
    empty. Mutually exclusive with PR_NUMBER/`--diff`/`--base`.

---

## Part 3 — Simplification pass (grounded in gate-review observations)

Verified against current `main`; each is a small, behavior-preserving
refactor. Tests must stay green throughout.

20. **Fix the layering smell: `cache` imports `analyze`.** The cache package
    depends on the analysis package for `DefaultStateDir` and
    `ValidateComplete` — a utility package reaching up into domain logic.
    Move state-dir resolution into a tiny `internal/xdg` (or `paths`)
    package both import; pass validation in as a `func(document.Document) error`
    from the caller (app already orchestrates both). Cache then depends only
    on `document`.
21. **Unify the two git runners.** `internal/source` and `internal/blame`
    each own a near-identical exec-git-capture-stderr runner. Extract one
    `internal/gitexec` used by both, carrying the shared hardening flags
    (`color.ui=false`, `--no-textconv`, ...) in one place so future flags
    can't drift between call sites.
22. **Deduplicate the fold algorithms.** `applyUnifiedFolds` and
    `applySplitFolds` are the same algorithm over two row types. One generic
    implementation (Go generics with a small row-accessor interface, or
    fold over indexes + predicate/mark callbacks).
23. **Share the path-layer heuristics.** The mock provider's canned analysis
    and staging's `pathLayerHint` both classify paths into layers with
    near-identical regex sets. One table in one place.
24. **Move test-only exports out of the public surface.**
    `viewer.IsPlainHighlighted` exists only for tests — relocate into the
    test package (or an internal testutil) so the package API is only what
    the product needs.
25. **Precompile staging regexes.** `pathLayerHint` calls
    `regexp.MustCompile` per invocation inside a per-file loop — compile
    once at package level. (Micro, but it's also the correctness-adjacent
    idiom: MustCompile in a hot path is a known Go smell.)

Explicitly NOT in scope: rewriting the viewer template, changing the
document schema, touching provider behavior, or any user-visible change
beyond Part 2's items.

### Part 3b — Architecture review findings (extensibility/maintainability
pass, owner-requested)

Assessment: `document` as a pure shared-vocabulary hub, the provider
contract, and the pure `diff`/`viewer` packages are sound. Four gaps keep
the code from being as extensible as the locked design claims:

27. **Make diff sources a real adapter layer.** Locked decision #7 promised
    forge adapters, but `internal/source` is hardcoded functions with `gh`
    baked in — adding GitLab today means editing core files. Introduce a
    `Source` interface + registry symmetric to providers (Name / Detect /
    Collect), with GitHub as the first adapter and git/patch as the core
    sources. New forges become one new file.
28. **Decompose `app.Run`.** It inlines args → config → source → provider →
    guard → cache → analyze → render → write; every new flag ripples
    through it. Split into pipeline stages with narrow inputs/outputs so
    additions stay local and stages are unit-testable.
29. **Single source of truth for the schema.** The structured-output JSON
    schema (`analyze.Schema`) and the Go types (`document`) describe the
    same shape in two places. Add a reflection-based sync test (or co-locate
    schema with the types) so they cannot drift.
30. **Encode the layering matrix in the policy test** (extends #15):
    `document` imports no internal packages; `diff`/`viewer`/`cache` depend
    only on `document` (+ small utils); provider adapters import only the
    provider contract + stdlib; nothing imports `app`. The architecture then
    defends itself against every future PR.
31. **(Recommended-optional) Provider adapter subpackages.** Move each
    adapter under its own package so isolation is structural, not
    conventional. Borderline at four adapters; leans yes given the
    OSS-contribution goal. Owner may defer.

## Locked answers (owner review, 2026-07-16)

1. Part 1: adopt ALL — the thirteen plus CLA (moved from skip).
2. Attestation: GitHub-native `actions/attest-build-provenance`.
3. Naming: **bgr is primary everywhere**; `better-git-review` ships as the
   long alias; repo name unchanged.
4. Windows CI: full e2e suite REQUIRED on windows-latest — real Windows
   users are waiting.
5. Part 3 approved in full; Part 3b added at owner's request (deeper
   architecture pass). #31 awaits an explicit yes/no.

---

## Part 4 — Dogfooding fixes (running list)

26. **BUG: diff rendering theme defects** *(batched into M4 by owner)*.
    Two related root causes, both diagnosed:
    - *Invisible add/del backgrounds:* chroma's generated CSS sets an opaque
      `background-color` on `.chroma`, and every code `<td>` carries that
      class — it paints over the row's translucent `--add-bg`/`--del-bg`,
      so only the line-number gutter tints. Fix: strip backgrounds from the
      generated chroma CSS.
    - *Near-black tokens in dark mode:* the light chroma stylesheet is
      embedded globally with the dark one overlaid in a media query; token
      classes the dark style doesn't fully override keep light-theme colors
      (`#24292e`-family) on the `#0d1117` canvas. Fix: theme the token
      palette via CSS custom properties (one set of token classes, two
      variable blocks) or fully-scoped stylesheets with a unit test
      asserting the dark sheet overrides every class the light sheet emits.
    M4 scope is CORRECTNESS (right colors, visible backgrounds, GitHub-dark
    parity per the owner's reference screenshots); aesthetic strengthening
    (left-edge accents, alpha tuning, density) belongs to the M5 design
    gate.

34. **Overview diagram is boring, small, and hard to read** *(owner,
    for the M5 design lane — see `reference/overview-diagram-current.png`)*.
    Current state: default-theme mermaid, tiny font, monochrome boxes, no
    connection to the rest of the page's visual language.
    **LOCKED (owner, 2026-07-16): native diagram.** Drop mermaid entirely;
    the M5 design gate renders a native HTML/SVG cohort-flow diagram from
    the VALIDATED data we already have (cohorts + layers + dependsOn)
    instead of the model's free-text mermaid string. Wins: layer badge
    colors reused, nodes clickable (jump to cohort step), consistent
    typography, kills the last CDN dependency (the HTML becomes fully
    offline/self-contained), and the diagram can never contradict the
    actual cohort structure. Consequences owned by M5: stop requesting the
    mermaid field in the analysis prompt, remove the CDN script + fallback,
    drop `analysis.mermaid` from the schema with a schemaVersion bump, and
    tighten the self-containment e2e allowlist to ZERO external references.

32. **"Viewed" per-file toggle** *(pulled from the deferred list by owner)*.
    GitHub-style checkbox in each file header; checking marks the file
    reviewed and collapses it; state persists in `localStorage` keyed by
    document identity + file path so a reopened walkthrough remembers
    progress; sidebar/step shows reviewed-count progress. Assigned to the
    **M5 design lane** (the file chrome is being redesigned there anyway —
    building it twice across M4/M5 would be waste).

*(more items land here as dogfooding continues)*

## Part 5 — Design workstream: new ownership split

**Process change (owner decision, 2026-07-16):** Codex-gate handoffs
continue to own everything EXCEPT design. Visual/UX design of the viewer is
owned by Claude directly (design-skill-assisted), working in the same
branch/PR discipline. Rationale: the target is the CodeRabbit-concept
aesthetic (see `reference/coderabbit-diff-viewer.png`), and design iteration
via engineering handoffs loses too much in translation.

**Design direction (owner preference):** move the viewer toward the
CodeRabbit reference — stronger diff color presence with left-edge accents,
vivid syntax palette on dark, tighter file chrome, the overall "guided
review tool" feel rather than "rendered document."

**Claude lane also owns docs voice** *(owner decision, 2026-07-16)*:
33. **Docs voice pass.** All public-facing markdown (README, CONTRIBUTING,
    SECURITY.md, release-note templates, and any docs/ added by M4) rewritten
    in the owner's voice via the `janior-voice` skill, modeled on the
    drawover repo's docs — less AI-ish, same substance. Part of the M4
    timeframe but sequenced AFTER the Codex M4 merge (M4 changes README
    content — bgr-primary, Windows, CLA — so the voice pass lands on final
    content, not churn). Internal working docs (PLAN*.md, handoffs/) stay
    as-is; they're process artifacts, not the public face.

**Boundary between the lanes:**
- *Claude (design lane):* everything inside `internal/viewer/template.html`'s
  CSS + markup structure and the visual parts of `viewer/*.go` rendering
  (chroma style choice, tokens, spacing, chrome). Direction locked with the
  owner via mockups BEFORE implementation.
- *Codex (engineering lane):* any new data the design needs (e.g. new
  document fields), behavioral JS, build/CI, and everything in Parts 1–3.
- Sequencing: design direction can be explored in parallel with the M4
  engineering gate; design implementation lands as its own gate (M5) on top.

**Design-skill inventory (pass done 2026-07-16)** — applicable skills and
their role in the M5 flow:
1. `interactive-mockup` — lock direction: 2–3 visual treatments of one real
   cohort step as clickable states, owner picks before any real work.
2. `ideas` — in-browser comparison/picker for finer calls (accent styles,
   density, file-chrome variants).
3. `design` / `ui` — the main build pass under the ui.sh guideline system.
4. `frontend-design` — production-grade polish pass; explicitly guards
   against generic-AI aesthetics.
5. `add-dark-mode` — dark theme done as surfaces/shadows/color systems
   rather than alpha-tweaks; directly addresses the washed-out dark diff.
6. `markup-from-image` — extract semantic structure from the CodeRabbit
   reference screenshot as a starting skeleton where useful.
7. `make-responsive` — the viewer's narrow-viewport story (currently the
   sidebar just hides).
Not applicable: `canonicalize-tailwind` (hand-written CSS, no Tailwind),
`brand-kit`/`dark-mode-image` (no brand/raster needs), `componentize`
(single Go-templated file; component extraction happens naturally in the
template structure).

## Suggested gate shape

- **Hotfix (now, no gate):** Part 4 #26 — the chroma background bug. One
  focused PR, fast merge, dogfooding immediately improves.
- **Gate M4 (Codex):** Parts 1–3 + accumulated Part 4 engineering items, in
  this order: Part 3 first (simplify on a quiet codebase before adding to
  it), then Part 2 (features on the cleaned base), then Part 1 (CI/release
  last so the new Windows job and policies validate the final state).
  Branch `gate/m4-polish`, PR, full testing policy (unit + e2e for every
  behavioral item: `--dirty`, `--open` per-OS dispatch table, bgr presence
  in snapshot archives, Windows paths, policy tests themselves).
- **Gate M5 (Claude, design lane):** direction lock via mockups → viewer
  redesign toward the CodeRabbit concept → PR under the same review
  discipline (owner reviews visually; e2e suite must stay green).

## Remaining open questions

*(none — all resolved)*

Further locked answers (owner, 2026-07-16): #31 provider adapter
subpackages — YES, adopt. #26 — batched into Gate M4, not hotfixed.
Owner-supplied reference screenshots for #26/#32 (current-vs-GitHub dark
diff, GitHub Viewed control): saved under `reference/`.
