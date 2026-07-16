# Gate M6 — Pointed Fixes (handoff)

You are implementing Gate M6 of `better-git-review`. Read `PLAN.md`
(design authority) and **`POINTED-FIXES.md`** at the repo root — it
carries the full owner-approved spec for every item below; item numbers
here refer to it. Where this handoff summarizes and POINTED-FIXES.md
details, the details win.

## Scope: the 12 Codex-lane items

You implement: **#1–#11 and #14** (machinery). You do NOT implement
#12/#13 (viewer navigation/theming — a separate Claude design PR runs in
parallel; do not restructure the viewer's toolbar, pager, or theme CSS
beyond what your items strictly require).

## Precondition & process

Branch `gate/m6-pointed-fixes` off current `main`. PR to `main`; leave
unmerged. Do not modify `PLAN*.md`, `POINTED-FIXES.md`, `prototype/`,
`reference/`, or `handoffs/`. Conventional commit prefixes. Document
deviations in the PR under "Deviations & decisions". Suite stays green
after each phase.

## Suggested phase order (dependency-driven)

**Phase A — sources & resilience:** #1 (gh retry + SHA-based local-git
fallback), #3 (`--head`, `--commit`), #8 (actionable gh-missing/unauthed
errors via Detect), #2 (patch-mode range shows basename, not the full
path).

**Phase B — models & reasoning:** #7 (baked defaults: claude-cli
`sonnet`; codex-cli `gpt-5.6-luna` reasoning `low`; openrouter
`z-ai/glm-5.2` — RE-VERIFY every slug/CLI model name against the live
catalog and installed CLIs at build time; reasoning config key +
`--reasoning` flag; cache key gains reasoning; TOFU fingerprints change —
expected, document it).

**Phase C — CLI UX:** #5 (auto-open by default per the TTY matrix),
#10 (styled progress + spinner; NO_COLOR + non-TTY byte-identical to
today — existing e2e proves it).

**Phase D — discovery & onboarding:** #4 (interactive picker `-i` +
empty-diff TTY entry; requires #3), #11 (first-run onboarding +
`bgr configure` subcommand + the `Cataloger` optional provider
interface; requires #7), #14 (embedded agent skill + optional install
via configure — ship a minimal functional SKILL.md clearly marked
"content pending docs voice pass"; the final text arrives with #33).

**Phase E — media & install:** #6 (image previews as render-time
enrichment, data-URI embedding, SVG strictly via <img>, self-containment
test updated for data: in img src only), #9 (checksum-verifying
install.sh + CI job exercising it against a goreleaser snapshot via a
local override — never a real release).

## Cross-cutting constraints

- Dependency allowlist unchanged: toml + chroma. Picker, spinner, color,
  onboarding are ALL stdlib (ANSI escapes, numbered prompts — no TUI
  frameworks).
- No live network in `go test ./...`: OpenRouter catalog fetches (for
  #11's Cataloger) are behind interfaces with canned fixtures; the live
  path is verified in your manual runs, not the suite.
- NO API keys anywhere: the owner's OpenRouter test key is disabled; do
  not expect one. Key handling stays env-var-by-name only.
- The policy tests (imports, deps) must stay green — new packages need
  layering entries consistent with the matrix.
- Windows CI runs the full suite; all new e2e must be path-safe.

## How to test/validate

Per PLAN.md testing policy: unit + e2e per item as specified in
POINTED-FIXES.md (each item carries its test list). Highlights the review
will weight heavily:
- #1: e2e with a fake `gh` shim that 503s → falls back to real refs in a
  throwaway repo; retry/backoff unit-tested.
- #4/#11: scripted-stdin e2e through picker and onboarding; fresh
  non-TTY runs behave EXACTLY as today (no onboarding, no picker, no
  color, no auto-open).
- #6: real changed PNG in a throwaway repo → both data URIs render;
  self-containment still passes; patch mode shows the honest label.
- #10: zero ANSI bytes in non-TTY output (byte-compare against current
  fixtures).

```sh
make verify
go test ./...        # no network
BGR_E2E_CLAUDE=1 go test ./test/e2e/ -run Claude -v -count=1
```

Include in the PR: a short terminal recording or pasted transcript of
(a) the styled TTY output with spinner, (b) the picker, (c) first-run
onboarding — the reviewer judges DX against the specs.

## Out of scope — do NOT build

#12/#13 (Claude design lane), #33 docs voice, MCP server (explicitly
deferred in #14), `--serve`, GitLab adapter (community by design), repo
layer hints (unapproved), any release/tag/public action.

## Acceptance checklist

- [ ] make verify green from clean checkout; CI green ubuntu+macos+windows
- [ ] All 12 items implemented per POINTED-FIXES.md with their tests
- [ ] Non-TTY behavior byte-compatible with today across #5/#10/#11/#4
- [ ] Default slugs/models re-verified live and noted in the PR
- [ ] Self-containment test still passes (data: URIs in img src only)
- [ ] TOFU fingerprint migration documented in PR body
- [ ] DX transcript/recording for spinner, picker, onboarding in PR body
- [ ] PR from `gate/m6-pointed-fixes`, unmerged, "Deviations & decisions"
