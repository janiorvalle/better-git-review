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

- **CLA workflow** — heavyweight for this project's stage; MIT + DCO-style
  sign-off is enough unless contribution volume changes. Revisit if it takes off.
- **size-limit budgets** — meaningful for a browser lib; a CLI binary's size
  is not a product constraint. The artifact smoke test covers "did we ship
  something sane."
- **changesets bot itself** — see #16.

---

## Part 2 — Platform polish (from dogfooding feedback)

17. **`bgr` binary, shipped natively.** Release archives contain both
    `better-git-review` and `bgr` (same main, second goreleaser build id, or
    a rename step in the archive). README documents both; `bgr` becomes the
    documented short form everywhere after the first mention.
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

---

## Part 4 — Dogfooding fixes (running list)

26. **BUG: diff add/del backgrounds invisible in the code area.** Diagnosed:
    chroma's generated CSS sets an opaque `background-color` on `.chroma`,
    and every code `<td>` carries that class — it paints over the row's
    translucent `--add-bg`/`--del-bg`, so only the line-number gutter tints.
    Both themes affected. Fix: strip `background-color` from the generated
    chroma CSS in `ChromaCSS()` (or override `.chroma { background:
    transparent }`), and revisit the dark alphas (0.15/0.10 are barely
    perceptible on `#0d1117` even once visible — CodeRabbit's reference
    reads much stronger, with a solid left edge accent per changed line).
    *Small enough to hotfix ahead of any gate.*

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

## Open questions for review

1. Part 1 adopt-list: all thirteen, or trim? (My take: all — each is cheap
   and this repo is headed public. The only debatable one is gitleaks'
   pre-commit hook, which adds a local tool dependency for contributors;
   CI-only scanning is the fallback position.)
2. Attestation (#8): GitHub-native attest action, or full SLSA generator?
   (My take: the attest action — one step, no new infra.)
3. `bgr` (#17): second binary in the same archive, or `bgr` as the primary
   name everywhere with `better-git-review` kept as the long alias? Naming
   is still repo-owner territory.
4. Windows e2e (#18): require the full subprocess e2e suite on the Windows
   CI job, or accept build + unit as the gate? (My take: require it — the
   suite is deterministic, and Windows path bugs live exactly in those
   subprocess/fixture seams.)
5. Anything in Part 3 you'd rather leave alone?
