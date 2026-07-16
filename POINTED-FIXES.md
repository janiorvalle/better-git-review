# Pointed fixes (post-M5 dogfooding)

Working list. Each item gets diagnosed and designed here; engineering items
become a Codex gate handoff when the list stabilizes.

## 1. GitHub PR diff endpoint failures hard-fail the run — needs local-git fallback

**Observed** (owner, 2026-07-16): `bgr 5 -C . --open` →
`gh pr diff: HTTP 503` — reproducible, NOT transient. PR #5 is only 12
files / +744−206 and `gh pr view` metadata works; GitHub's diff endpoint is
simply unreliable for some PRs (it also hard-caps PRs over 300 files, and
503s diffs it considers expensive).

**Fix (github source adapter):**
1. Retry `gh pr diff` briefly on 5xx (2 retries, short backoff) — handles
   genuinely transient blips.
2. On persistent failure OR >300 changed files: **local-git fallback** —
   `gh pr view --json baseRefOid,headRefOid` (metadata endpoint is
   reliable), `git fetch origin <oids>` in the repo dir via the hardened
   gitexec, then diff `baseOid...headOid` locally. Object-store only; never
   touches the working tree.
3. No usable local repo → error that explains BOTH failures and suggests
   running from a clone.
- Side effect: makes huge PRs (ekualiti #4533, 473 files) reviewable via
  PR number, completing the intent of PLAN decision #7.
- Tests: unit (retry/backoff, fallback decision), e2e (fake `gh` shim that
  503s → falls back to a real throwaway repo's refs).

**Lane:** Codex (source adapter engineering).

## 2. Patch-mode "range" shows the full ugly path (carried from M5 visual pass)

Rail shows e.g. `/private/tmp/claude-501/.../scratchpad/test.patch` as the
range. Patch source should present basename as the range/title detail and
keep the full path out of the rail (tooltip or omit).

**Lane:** Codex (source metadata; one-line viewer tweak may ride along).

## 3. Git source is hardwired to "...HEAD" — add arbitrary refs and single-commit review
*(approved by owner, 2026-07-16)*

**Coverage audit:** branch-vs-base ✅ (`--base`), dirty ✅ (`--dirty`),
patch/stdin ✅ — but branch A vs branch B (neither checked out) and
"review just this one commit" both require piping `git diff` manually.

**Fix (git source):**
- `--head <ref>` — pairs with `--base`; diff is `<base>...<head>` instead
  of `<base>...HEAD`. Default remains HEAD (zero behavior change).
- `--commit <sha>` — sugar for reviewing exactly one commit
  (`<sha>^..<sha>`); mutually exclusive with PR_NUMBER/--diff/--base/
  --head/--dirty.
- Blame: `--head`/`--commit` use committed-side coordinates against the
  head ref (not HEAD).
- Range/Title metadata reflects the actual refs.
- Tests: unit (arg exclusivity, ref resolution), e2e (throwaway repo:
  A-vs-B across two branches; single-commit review shows only that
  commit's changes).

**Lane:** Codex.

## 4. Interactive picker: `bgr -i` — "review what?" discovery
*(approved by owner, 2026-07-16, incl. filter loop and empty-diff entry)*

**Entry points:**
- `bgr -i [query]` — explicit; optional query pre-filters.
- Bare `bgr` finding an EMPTY diff in a TTY drops into the picker instead
  of erroring (non-TTY keeps the hard error; scripts unaffected).
- Bare `bgr` with a real diff is UNCHANGED (fast path stays fast).

**DX:** one-screen numbered sections, recency-capped at ~8 each:
WORKING TREE (dirty changes, if any) · OPEN PULL REQUESTS (via `gh pr
list`, most recently updated; section silently absent with a one-line note
if gh unavailable) · BRANCHES ahead of the detected base (by last commit
date) · RECENT COMMITS on HEAD. Hidden counts shown
("… 214 more PRs — type to filter").

**Prompt loop (stdlib only, no TUI dep):** number = select · `q` = quit ·
anything else = case-insensitive substring filter over the FULL sets (PR
titles/numbers, branch names, commit subjects), re-render narrowed. No
fuzzy scoring in v1.

**Selection maps onto existing sources** (nothing new in the pipeline):
dirty → `--dirty`; PR → PR number; branch → `--base <detected>
--head <branch>`; commit → `--commit <sha>`. Requires fix #3. After
selection, print the equivalent command ("→ running: bgr --commit
4e5755a") so the picker teaches the CLI.

**Tests:** unit (filter, section capping, mapping); e2e (scripted stdin
through the picker in a throwaway repo; empty-diff TTY entry; non-TTY
empty diff still errors).

**Lane:** Codex.

## 5. Auto-open the walkthrough by default
*(owner directive, 2026-07-16)*

Today `--open` is opt-in; flip it: generating an HTML walkthrough in an
interactive TTY opens it automatically. Matrix:
- HTML + TTY → auto-open (new default)
- HTML + non-TTY (scripts/CI/piped) → no open; explicit `--open` forces
- `--no-open` suppresses in a TTY (replaces `--open` as the common flag;
  `--open` retained for the non-TTY force case)
- `--format json` never opens
- Unsupported OS for the open dispatch → silent skip (existing behavior)
- Tests: unit for the decision matrix; e2e non-TTY runs must not attempt
  to open (assert via the existing per-GOOS dispatch seam).

**Lane:** Codex.

## 6. Binary files render as a bare "Binary file" — add image previews
*(approved by owner, 2026-07-16; confirmed still present post-M5 by repro)*

**Image preview** (binary + image ext: png/jpg/jpeg/gif/webp/svg, local
repo available — git modes, --dirty, and PR mode once #1's local fallback
lands): extract old/new blobs via gitexec, embed as base64 data URIs,
render a before/after card:
- two panes side by side, checkerboard backdrop for transparency,
  byte size + pixel dimensions caption per side ("96.2 KB → 41.7 KB");
  added/deleted images show a single pane with hatched placeholder on the
  missing side (match split-view language)
- cap ~1.5 MB per side; over cap → no preview, size delta only
- SVG strictly via <img src="data:image/svg+xml;base64,..."> — scripts do
  not execute inside <img>; NEVER inline SVG markup (XSS posture)
- data URIs preserve the zero-external-references guarantee (update the
  self-containment test allowlist for data: in <img> src only)

**Placement:** render-time enrichment ONLY — not in the document schema,
not in the JSON island, not in the cache (keeps document pure diff data,
no schema bump; previews still work on cache hits since render re-extracts
from the repo each run).

**Blob refs per mode:** base...head modes use the resolved refs; --dirty
old side = HEAD blob, new side = worktree file (read directly, same cap).

**Non-image binaries + all modes:** upgrade the label to a size story via
`git cat-file -s` where a repo exists ("Binary file · 1.2 MB → 1.3 MB
(+84 KB)"); patch/stdin mode: "Binary image · content not available from
patch input."

**Tests:** unit (cap, SVG-as-img, ref selection per mode); e2e (throwaway
repo with a real changed PNG → both data URIs present, self-containment
still passes; patch mode → honest label, no preview).

**Lane:** Codex.

## 7. Per-provider default models + a reasoning dimension
*(approved by owner, 2026-07-16; OpenRouter default tested live)*

**Baked defaults** (all user-overridable via config/flags):
- claude-cli: `sonnet`
- codex-cli: `gpt-5.6-luna`, reasoning `low` (owner: "light")
- openrouter: `z-ai/glm-5.2` (owner choice, open-weight; slug verified
  against the live catalog 2026-07-16; 1M context — delays staged
  analysis nicely; structured-output path verified end-to-end with a real
  key: valid v4 document, sane cohorts + dependency chain)
- Implementing agent MUST re-verify default slugs/CLI model names against
  the live OpenRouter catalog and installed CLIs at build time.

**Reasoning dimension:**
- `reasoning = "<level>"` joins `model` in each `[providers.*]` config
  block; new `--reasoning <level>` CLI flag beside `--model`.
- Adapter-validated, per-capability: codex-cli →
  `--config model_reasoning_effort="<level>"`; openrouter → request
  `reasoning: {effort}`; claude-cli → best-effort (verify installed CLI;
  if unsupported, warn once on stderr and proceed — never fail).
- Cost-guard/provider announcement line includes reasoning.

**Ripples (must handle):**
- Cache key gains reasoning (same diff+model, different reasoning =
  different analysis).
- ProviderConfig field addition changes TOFU fingerprints → existing
  repo-config users re-prompt once (expected, document in PR).
- README/CONTRIBUTING config examples updated to the new defaults.

**Tests:** unit (flag/config precedence incl. reasoning, cache-key
inclusion, adapter validation per provider); e2e (mock provider records
reasoning passthrough; announcement line shows it).

**Lane:** Codex.

## 8. Forge-less UX: actionable failure when gh is missing/unauthenticated
*(approved by owner, 2026-07-16; repro'd — raw exec error, exit code
correct)*

Core is already forge-agnostic (git refs, --dirty, patch/stdin need no
GitHub); only `bgr <PR#>` touches gh. But its failure today is raw
plumbing: `exec: "gh": executable file not found in $PATH`.

**Fix:**
1. PR mode consults the github source's existing `Detect()` BEFORE
   Collect and fails actionably, distinguishing:
   - gh not installed → "PR mode needs the GitHub CLI — install from
     cli.github.com, then `gh auth login`."
   - gh installed but unauthenticated (`gh auth status`) → "run
     `gh auth login`."
   Both end with: "Not using GitHub? Review by ref instead: --base/--head,
   --commit, --dirty, or pipe any diff via --diff -."
2. README states plainly that GitHub is optional (core forge-agnostic;
   PR-by-number is the only gh feature).
3. CONTRIBUTING gains a "writing a source adapter" guide beside the
   provider one (GitLab et al. stay community contributions by design).

**Tests:** e2e with gh stripped from PATH → exit 1 + message names the
install step and the ref alternatives; unit for the detect branching.

**Lane:** Codex.

## 9. install.sh + install-first README pattern
*(approved by owner, 2026-07-16 — future state; activates when the repo
goes public and v1.1.0 is tagged)*

**install.sh** (repo root, fetched via
`curl -fsSL .../main/install.sh | sh`):
- detects OS/arch (darwin/linux × amd64/arm64), fetches the latest
  GitHub Release archive
- **verifies the download against the release's checksums.txt before
  installing** — non-negotiable given the supply-chain posture; abort
  loudly on mismatch
- installs `bgr` (+ `better-git-review` alias) to `~/.local/bin`, prints
  a PATH hint if needed; `--version` smoke at the end
- no sudo, no /usr/local writes; idempotent re-runs
- Windows: not served by the script — README points at Releases zips
- Tests: shellcheck-clean; CI job exercises it against a goreleaser
  snapshot dist via a file:// or local-server override so it never needs
  a real release

**README top-matter**: install block moves to the very top (curl one-liner
/ Releases / go install), before any concept prose — layout lands with the
docs voice pass (#33), which this item feeds.

**Lane:** Codex (script + CI); README layout via #33 (Claude).

## 10. Terminal output: styled progress with spinner
*(approved by owner, 2026-07-16)*

Replace the teletype mumble with a structured progress display:

```
bgr review
  ◆ source     PR #4569 · ekualiti-kc  (via gh)
  ◆ parsed     18 files  +2,878 −14
  ◆ provider   claude-cli / sonnet
  ⠸ analyzing…                       (spinner + elapsed time)
  ✓ 6 cohorts  in 1m 12s
  ✓ wrote      walkthrough-pr-4569.html  → opened in browser
```

- **Spinner with elapsed time during analysis** — the highest-value part;
  the 60–90s LLM wait must never look hung. Staged mode: show call
  progress ("summarizing 14/350 …").
- Palette restraint: dim labels / bold values; green + and red − stats;
  indigo accent on the active line; ✓ / ✗ / ⚠ outcomes. Nothing else.
- Pure ANSI escapes — NO new dependencies.
- **Discipline (non-negotiable):** styling + spinner only when stderr is
  a TTY; honor NO_COLOR; non-TTY output stays byte-identical to today's
  plain lines (e2e assertions and user scripts unaffected). Decoration
  layered over the existing logf, never different words.
- Errors: red ✗ prefix, message text unchanged.

**Tests:** unit for the TTY/NO_COLOR gating (no ANSI bytes when off);
existing e2e untouched proves the non-TTY contract.

**Lane:** Codex.

## 11. First-run onboarding + `bgr configure` + model/reasoning discovery
*(approved by owner, 2026-07-16)*

**First-run onboarding** — bare `bgr`, interactive TTY, no user config yet:
welcome line → provider probe display (✓/✗ per provider) → default
provider → model → reasoning (only if supported) → auto-open — every
prompt Enter-accepts a sensible default (~15s happy path) → writes
`~/.config/better-git-review/config.toml` → quick-reference cheat sheet →
"review current branch now? [Y/n]" rolling straight into a first
walkthrough when in a repo with changes.
Guards: TTY-only; never in CI/scripts; any explicit flags skip it (with a
one-line `bgr configure` hint); `--yes` skips; never fires once config
exists.

**`bgr configure`** — first subcommand (non-numeric positional is
currently an error, so unambiguous). Same flow, re-runnable, pre-filled
with current values. Includes per-provider model AND reasoning (#7), and
OpenRouter key guidance (names the env var, never stores the key).
Deliberately does NOT touch repo-level config (keeps the TOFU-guarded
surface out of the wizard).

**Model/reasoning discovery — new optional provider interface** (same
pattern as StructuredProvider):
```go
type Cataloger interface {
    Models(ctx) ([]ModelOption, error) // {ID, Label, Note, Default}
    ReasoningLevels() []string         // empty = unsupported
}
```
- claude-cli: curated list (sonnet recommended / opus / haiku) + effort
  levels only if the installed CLI supports them (verify at build time).
- codex-cli: curated (gpt-5.6-luna recommended / terra / sol) + real
  levels (minimal/low/medium/high, low default).
- openrouter: LIVE catalog via the public models endpoint through the
  fix-#4 filter loop, rows show $/M + context window; offline → small
  curated fallback.
- EVERY list ends with "other… (type any model id)"; unknown IDs are
  passed through, never hard-rejected (curated lists go stale).
- Future providers: implement Cataloger → appear in configure for free;
  otherwise free-text fallback. Document in CONTRIBUTING as
  encouraged-optional.

**Tests:** unit (first-run detection, Enter-path defaults, Cataloger
fallbacks); e2e (scripted stdin through onboarding writes valid config;
fresh non-TTY run does NOT onboard and behaves exactly as today;
`configure` round-trips existing values).

**Lane:** Codex.

## 12. Step navigation ergonomics: ambient next/prev
*(approved by owner, 2026-07-16)*

Complaint: bottom pager is bulky, and moving on requires scrolling to the
bottom (fine when you're there, bad from anywhere else).

- **Sticky toolbar gains a compact pager cluster**: `‹ · Step 4 of 9 · ›`
  beside the step counter — next/prev always one click, no scrolling;
  visual language mirrors the file stepper.
- **Bottom pager slims down**: single low-profile row, ghost prev/next
  titles, no heavy bordered boxes; a disabled side is absent, not an
  empty box. Keeps the natural "finished reading → continue" affordance.
- **Keyboard discoverability**: subtle `← →` hint beside the toolbar
  cluster (arrow-key nav already exists; nobody knows).

**Lane: Claude (design)** — implemented as a small design PR alongside the
Codex gate once the list closes.

## 13. Manual theme toggle: Auto · Light · Dark
*(approved by owner, 2026-07-16)*

Today the viewer follows prefers-color-scheme only — no override. Add a
three-state control in the toolbar beside Unified/Split:
- `Auto` (default — follows OS, current behavior preserved) / `Light` /
  `Dark`; persisted in localStorage as a viewer-wide preference (not
  per-document).
- Implementation: restructure theming so tokens (surfaces AND injected
  chroma variables) resolve from `data-theme="light|dark"` attributes,
  with the media query powering only Auto. The M4 theme-coverage test
  guards the refactor.

**Lane: Claude (design)** — batched with #12 into the design PR.

## 14. Embedded agent skill + optional install via configure; MCP deferred
*(approved by owner, 2026-07-16)*

**Decision: no MCP server for now.** bgr's agent consumers have shells, and
`--format json` already emits a schema-versioned document designed as the
machine contract — the CLI is the API. An MCP wrapper over the same
contract is deferred until a shell-less host matters; note it in
CONTRIBUTING as a welcome community contribution.

**Agent skill (SKILL.md), embedded in the binary** so it always matches
the installed version. Content: when to reach for bgr (review a
PR/diff/commit request), mode selection (`--commit` for "review my last
commit", `--dirty` pre-commit, `--format json` for analysis-as-data,
`-i` never in non-TTY), reading the document schema (cohorts/dependsOn/
reviewNotes as a review structure), etiquette (`--yes` non-interactive,
cost guard expectations, don't --no-cache gratuitously).

**Install UX** — in onboarding (#11) and `bgr configure`:
Claude Code (~/.claude/skills/bgr/) / Codex (verify current skills-dir
convention at build time) / Both / Skip — **default Skip** (modifying
agent behavior is explicit-opt-in). Idempotent reinstall/upgrade on
re-run; uninstall = delete the folder, say so.

**Tests:** unit (embed present, install paths, idempotency); e2e
(configure with scripted stdin installs to a fake HOME; skip default
installs nothing).

**Lane:** Codex (install machinery); skill TEXT drafted by Claude
(janior-voice, part of #33 docs pass) and handed to the gate as content.

*(more items land here as they arrive)*
