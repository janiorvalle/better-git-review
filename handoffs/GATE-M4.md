# Gate M4 — Hardening, Platform, Simplification (handoff)

You are implementing Gate M4 of `better-git-review`. Read `PLAN.md` (v1
design authority) and **`PLAN-M4.md`** (this gate's approved plan — item
numbers below refer to it) at the repo root first. Reference screenshots
for the rendering bug live in `reference/`.

Two items in PLAN-M4 are explicitly NOT yours: #32 (Viewed toggle) and #33
(docs voice pass) — they belong to a separate design/docs lane. Also not
yours: any *aesthetic* redesign of the viewer. Item #26 is yours for
CORRECTNESS only (right colors, GitHub-dark parity per the reference
screenshots) — no visual restyling beyond that.

## Precondition & process

Branch `gate/m4-polish` off current `main`. PR to `main`; leave unmerged.
Do not modify `PLAN*.md`, `prototype/`, `reference/`, or `handoffs/`.
Document deviations in the PR under "Deviations & decisions". Commit
incrementally with conventional prefixes (`feat:`/`fix:`/`refactor:`/
`docs:`/`ci:`) — the release changelog groups by them (#16).

Work the three phases IN ORDER — each phase leaves the suite green:

## Phase A — Simplification & architecture (PLAN-M4 #20–25, #27–31)

Behavior-preserving unless stated. Tests stay green after every item.

1. (#20) Break the `cache → analyze` dependency: move state-dir resolution
   to a small shared package (e.g. `internal/xdg`); inject document
   validation into the cache as a function value from the caller.
2. (#21) One `internal/gitexec` runner used by `source` and `blame`,
   carrying the shared hardening flags in one place.
3. (#22) One generic fold implementation replacing
   `applyUnifiedFolds`/`applySplitFolds`.
4. (#23) One shared path-layer heuristic table (mock provider + staging).
5. (#24) Remove test-only exports from public package surfaces
   (`viewer.IsPlainHighlighted` et al → test/internal testutil).
6. (#25) Precompile the staging regexes at package level.
7. (#27) **Source adapter layer**: introduce a `Source` interface +
   registry symmetric to providers (Name / Detect / Collect). Local git and
   patch/stdin are core sources; GitHub (`gh`) becomes the first forge
   adapter. CLI behavior unchanged.
8. (#28) Decompose `app.Run` into pipeline stages (collect → parse →
   select → analyze-with-cache → render → emit), each unit-testable.
9. (#29) Schema sync test: reflection-based assertion that
   `analyze.Schema` matches the `document` types (or co-locate schema with
   types) so they cannot drift.
10. (#30+#15) **Policy-as-test** (`internal/policy` or similar, plain
    `go test`): go.mod dependency allowlist (toml, chroma + transitives);
    allowed-imports matrix — `document` imports no internal packages;
    `diff`/`viewer`/`cache` depend only on `document` (+ utils); provider
    adapters import only the provider contract + stdlib; nothing imports
    `app`.
11. (#31) Provider adapters into subpackages (contract + registry stay in
    `internal/provider`; each adapter under its own package). Policy test
    enforces adapter isolation.

## Phase B — Platform & product (PLAN-M4 #17–19, #26)

12. (#17) **bgr is the primary name**: `cmd/bgr` builds binary `bgr`;
    release archives ship both `bgr` and `better-git-review`; `--help`,
    usage strings, and README lead with `bgr`.
13. (#18) **First-class Windows**: `--open` works on Windows
    (`cmd /c start` or equivalent); config/state/cache paths use
    `os.UserConfigDir()`/`os.UserCacheDir()` on Windows — unix paths MUST
    NOT change; all e2e fixtures/tests made path-separator-safe.
14. (#19) **`--dirty` flag**: review only uncommitted changes
    (`git diff HEAD`) regardless of branch state; mutually exclusive with
    PR_NUMBER/`--diff`/`--base`; blame uses the existing uncommitted
    (old-side vs HEAD) coordinates.
15. (#26) **Theme correctness bugs** (see reference/ screenshots):
    strip backgrounds from generated chroma CSS so add/del row colors show
    through; retheme the token palette via CSS custom properties (single
    set of token classes, light + dark variable blocks) OR fully-scoped
    stylesheets with a unit test asserting the dark sheet overrides every
    class the light sheet emits. Target: GitHub-dark parity per
    `reference/github-dark-diff-viewed-control.png`. No other visual
    changes.

## Phase C — CI, release, repo hygiene (PLAN-M4 #1–16)

16. (#14) `Makefile` with `make verify` = build + vet + test + goreleaser
    check + artifact smoke; CI calls the same target.
17. (#1–#6) Rework workflows to drawover's standard: top-level
    `permissions: {}` with commented per-job grants, SHA-pinned actions
    with version comments, `persist-credentials: false`, concurrency
    groups (CI cancels in progress; release never), `timeout-minutes`
    everywhere, fork guard on release.
18. (#7) Release job runs in a protected `environment: release` (document
    the environment-creation step for the owner — repo settings are not
    yours to change).
19. (#8) `actions/attest-build-provenance` on goreleaser artifacts;
    checksums retained.
20. (#9) gitleaks: `.gitleaks.toml`, pre-commit hook (installed via a
    make target, not npm), and a CI secret-scan job.
21. (#10) `SECURITY.md` disclosure policy.
22. (#11) Scorecard workflow (dormant until public).
23. (#12) Docs-only path filter so Markdown-only PRs skip build/test jobs.
24. (#13) Artifact smoke test in CI: goreleaser snapshot → unpack → run
    `bgr --version` and one `--diff fixture --provider mock` walkthrough.
25. (#adopted CLA) Port drawover's CLA workflow, adapted to this repo.
26. (#18) Windows CI job: `windows-latest` running the FULL suite including
    e2e — this is required, not optional; real Windows users are waiting.

## How to test/validate (per PLAN.md testing policy)

Unit: every Phase A refactor keeps existing tests green and adds tests
where seams changed (gitexec, fold generic, source registry/detection,
pipeline stages, schema sync, policy tests themselves, chroma theme
coverage test). Phase B: unit + e2e for `--dirty` (committed+dirty tree →
only uncommitted reviewed), `--open` dispatch table (per-GOOS, no
execution), bgr/alias presence in snapshot archives, Windows-safe paths.
Phase C: workflows validated with actionlint or zizmor if available
(document if not), `make verify` green locally, artifact smoke green in CI.

```sh
make verify          # the whole local gate
go test ./...        # deterministic, no network
BGR_E2E_CLAUDE=1 go test ./test/e2e/ -run Claude -v -count=1
```

CI on the PR must be green on ubuntu + macos + windows.

## Out of scope — do NOT build

Viewer redesign/aesthetics beyond #26 correctness, Viewed toggle (#32),
docs voice rewrite (#33), GitLab adapter (the source layer only needs
GitHub ported), publishing tags/releases, flipping the repo public.

## Acceptance checklist (the review will check exactly this)

- [ ] `make verify` green from a clean checkout; CI green on all three OSes
      (Windows runs the full e2e suite)
- [ ] Phase A: layering fixed (policy test proves the import matrix),
      source adapter layer in place, app pipeline decomposed, schema sync
      test present, adapters in subpackages
- [ ] Phase B: `bgr` primary everywhere + both names in archives; Windows
      `--open` + native paths (unix unchanged); `--dirty` with e2e; #26
      fixed with the theme-coverage test, verified against the reference
      screenshots in both themes
- [ ] Phase C: all workflow hardening items present (pinned SHAs, least
      privilege, guards, timeouts, concurrency); environment + attestation
      + gitleaks + SECURITY.md + Scorecard + CLA + docs-only filter +
      artifact smoke
- [ ] Conventional commit prefixes throughout
- [ ] PR from `gate/m4-polish`, unmerged, with "Deviations & decisions"
      and before/after screenshots for #26 (both themes)
