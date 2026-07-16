# pr-walkthrough — v1 Plan

**Status:** planning · **Last updated:** 2026-07-16

## Agreed scope (things we've actually discussed)

1. **The product**: a CLI, run locally, that turns a PR/diff into an HTML file
   you open in a browser — a CodeRabbit-style guided walkthrough: the change
   broken into meaningful blocks, clustered into intent-based cohorts, ordered
   logically (e.g. schema → backend → API → UI → tests), presented with scoped
   summaries, navigable diffs, and occasional diagrams.
2. **GitHub-fidelity diff rendering**: the diffs should look like GitHub's PR
   "Files changed" view — same fonts, same green/red diff colors, and real
   syntax highlighting.
3. **Language: Go** *(locked 2026-07-16)* — single static binary; `chroma`
   for server-side GitHub-style syntax highlighting.
4. **Analysis engine: pluggable provider layer** *(locked 2026-07-16)* — no
   hard dependency on any one LLM. A provider interface (think: adapter per
   backend) so users can plug in Claude (CLI or API), OpenRouter, Codex, or
   anything else. We don't pick winners; we define the contract.
5. **Open source, MIT, public repo** *(locked 2026-07-16)* — build with that
   destination in mind (no internal assumptions baked in, clean history,
   provider-agnostic by design per #4).
6. **Zero-config: auto-detect agent CLIs** *(locked 2026-07-16)* — with no
   provider configured, probe for installed agent CLIs (claude, codex, ...)
   and use the first found; if none, fail with a clear error naming the
   config options.
7. **Diff-source layer** *(locked 2026-07-16)* — forge-agnostic core: local
   git refs and patch files/stdin always work, for any forge. Forges (GitHub,
   GitLab, ...) are optional adapters that only add PR-number resolution and
   title/description metadata. GitHub adapter ships first. Side effect: the
   300-file GitHub API cap stops mattering — local git is the primary path
   for big PRs.
8. **Staged analysis for huge diffs** *(locked 2026-07-16)* — when a diff
   exceeds the single-pass budget: summarize per file/directory first, then
   cluster from the summaries. Full diffs still render in the HTML; only the
   analysis is staged.
9. **Viewer feature set** *(locked 2026-07-16)* — all four prototype pillars
   carry into v1: step navigation (sidebar + prev/next + keyboard), overview
   page with summary/TOC/relationship diagram, collapsible per-file diffs
   (large files start folded), and single self-contained HTML output.
10. **Full GitHub diff parity** *(locked 2026-07-16)* — unified view,
    word-level intra-line highlights, AND a side-by-side split view toggle,
    with GitHub's fonts, colors, and syntax highlighting throughout.
11. **Name: `better-git-review`** *(locked 2026-07-16)* — public repo name
    and binary name. (Note the name is forge-neutral — fits decision #7.)
12. **Distribution: GitHub releases** *(locked 2026-07-16)* — prebuilt
    binaries per OS/arch (goreleaser). Homebrew/`go install` can come later
    if wanted; releases are the v1 channel.
13. **v1 IS the product** *(locked 2026-07-16)* — no cut-down "usable next
    week" v1 with fast-follows. Everything locked above ships in v1; if it's
    not fully usable day one, it won't get used and there is no v2.
    Milestones below are build-dependency order, not shippable slices.
14. **From the CodeRabbit reference** *(locked 2026-07-16)* — IN for v1:
    cohort dependency chips ("depends on <cohort>", one schema field + a UI
    chip) and per-hunk blame attribution (author + date, local-git sources
    only — patch/API sources render without it). DEFERRED: semantic delta
    labels, within-cohort file stepper.

Everything else below is either an observed fact from the prototype or an
open decision.

## Facts learned from the v0.1 prototype

A throwaway Node prototype exists in this repo (`cli.js`, `template.html`) and
was run against real diffs (bpm-engine, ekualiti-kc PR #4569). What testing
established:

- **GitHub's API refuses diffs for PRs over 300 files** (`gh pr diff` → HTTP 406).
  ekualiti-kc #4533 (473 files, +218k lines) is unreachable that way; any
  large-PR story needs a different diff source.
- **A very large diff can't fit in a single LLM prompt**, so one-shot analysis
  has a ceiling regardless of which model/engine is chosen.
- **Free-form LLM JSON output is fragile** — one real run produced invalid JSON
  (unescaped quote in a prose field). Whatever engine is chosen needs a
  validation/repair/retry story or structured output.
- The walkthrough format itself (overview → ordered cohort steps with
  narratives, review notes, and inline diffs) got a positive first reaction on
  a real PR — treat the prototype as the reference point to react against, not
  as decided UX.

## Reference: CodeRabbit's viewer (`reference/coderabbit-diff-viewer.png`)

Screenshot of CodeRabbit's Change Stack viewer, provided 2026-07-16 as a
design reference for the diff/walkthrough UI. What it shows, mapped to our
decisions:

**Confirms things we already locked:**
- Left sidebar of numbered cohorts ("Layers") with per-cohort file-count
  badges; Overview entry on top → our step navigation (#9)
- Side-by-side split view with line numbers on both sides, syntax
  highlighting, dark theme → our full diff parity (#10)
- Cohort title + one-line scoped description above the files → our cohort
  narrative

**Patterns we haven't discussed (candidate ideas, NOT locked):**
- *Cohort dependency chips* — "Depends on: <other cohort>" next to the cohort
  title, making the reading order's *why* explicit
- *Unmodified-region folding* — "39 unmodified lines" collapsed bars between
  hunks, expandable in place (GitHub does this too; fits parity goal)
- *File stepper* — "File 1 of 4" prev/next within a cohort, plus a
  mark-all-viewed control
- *Per-hunk attribution* — author + date strip above each change ("who last
  touched this line region")
- *Semantic change labels* — a "Delta" chip classifying a change
  (e.g. "structural · lines 252-266")
- *Hatched placeholder* on the empty side of an add/delete in split view

## Open decisions

Nothing here is decided. Recommendations are marked where I have one.

### Viewer details (smaller, decide during build)
Locked pillars aside (agreed scope #9–10, #14), some small choices remain:
"viewed" checkboxes per file, dark/light behavior, diagram renderer
(mermaid-from-CDN vs. something embedded — matters for the self-contained
+ offline story).

## Design drafts (for review — not locked until approved)

### Provider interface contract *(reviewed & locked 2026-07-16)*

Providers are deliberately dumb: **prompt in, text out**. The core owns
prompt construction, JSON extraction, schema validation, retries, and
staging — so a contributed provider is ~50 lines, and quality is uniform
across providers.

```go
type Provider interface {
    Name() string                      // "claude-cli", "codex-cli", "openrouter"
    Detect() (available bool, detail string)   // e.g. found path / missing key
    Complete(ctx context.Context, prompt string) (string, error)
}

// Optional upgrade (locked: structured output from day one). Providers that
// can force schema-valid JSON natively (API backends) implement this; the
// core detects it via type assertion and skips the repair/retry machinery.
type StructuredProvider interface {
    Provider
    CompleteStructured(ctx context.Context, prompt string, schema json.RawMessage) (json.RawMessage, error)
}
```

- **Registry & detection order** (zero-config, per agreed scope #6):
  `claude-cli` → `codex-cli` → `openrouter` (env key present) → error listing
  what was probed and how to configure. Explicit config always wins over
  detection. The chosen provider + model is always announced on stderr
  before any call — no silent spend.
- **v1 providers**: `claude-cli`, `codex-cli`, `openrouter` (direct API,
  implements `StructuredProvider`). Everything else is a contribution
  opportunity — the OSS point of the layer.
- **Config**: TOML. User-level `~/.config/better-git-review/config.toml`,
  optional repo-level `.better-git-review.toml` (repo overrides user), flags
  override both. Example:

  ```toml
  provider = "openrouter"

  [providers.openrouter]
  model = "anthropic/claude-sonnet-4-5"
  api_key_env = "OPENROUTER_API_KEY"   # name of env var, never the key itself

  [providers.claude-cli]
  model = "sonnet"
  ```

- **Repo-config trust (locked: full config + first-run prompt)**: repo-level
  config may set anything, including provider/endpoint — but the first time
  a repo's config wants to change the provider, endpoint, or key env var,
  the tool shows exactly what it wants and asks for confirmation. The
  approval is remembered (fingerprint of the provider-relevant config stored
  in user state); any later change to those fields re-prompts. Protects
  against a malicious cloned repo silently exfiltrating diffs to an
  attacker-controlled endpoint.
- **Cost guardrail (locked: show plan + confirm)**: when an analysis will
  exceed a call-count threshold (staged analysis on huge diffs), print the
  plan — call count, provider, model — and require interactive confirmation
  or `--yes`.
- **Still deferred**: streaming, other capability flags.

### Output/robustness contract *(reviewed & locked 2026-07-16)*

Bad model output must never produce a broken page. Every `Complete()` result
runs the same gauntlet:

1. **Extract** — strip fences/prose, isolate the JSON candidate.
2. **Parse**, and on failure a **repair pass** (escape unescaped quotes /
   raw newlines inside strings — both observed in prototype testing).
3. **Schema-validate** against a real JSON Schema (cohorts, layers enum,
   dependency references, file indexes).
4. **Retry** — on parse/validation failure, re-prompt once with the
   validator's errors quoted back. Max 2 model calls per analysis unit.
5. **Deterministic seatbelts** (post-validation, in code): every file lands
   in exactly one cohort (unassigned → "Other changes" catch-all), invalid
   layer → `other`, dependency chips referencing unknown cohorts dropped.
6. **Hard fail** — if still invalid, exit nonzero with the raw output saved
   to a debug file under `~/.local/state/better-git-review/` (path printed
   in the error). Never render a half-broken walkthrough. This applies to
   the clustering pass too (locked): no heuristic-grouping fallback — a
   folder-grouped page without narratives isn't the product.

`StructuredProvider` backends skip stations 1–2 (shape-valid by
construction) but still run 3–5 — forced JSON guarantees shape, not
semantics (e.g. out-of-range file indexes).

Staged analysis (huge diffs, agreed scope #8) applies the same gauntlet to
each per-file summary call; a file whose summary fails after retry enters
clustering with a path-derived stub summary, flagged in the HTML.

**Caching (locked: in v1)** — analysis results cached keyed on
`hash(diff) + provider + model + tool schema version`, stored in the XDG
state dir. Same diff re-renders instantly and free; viewer iteration never
re-bills; any content change invalidates naturally. `--no-cache` forces a
fresh run.

## Milestones (build-dependency order; v1 ships only when all are done)

- **M1 — Core pipeline**: Go skeleton; diff sources (local git refs, patch
  file/stdin, GitHub adapter via `gh`); unified-diff parser; provider layer
  with the three v1 providers (incl. repo-config trust prompt + cost
  guardrail); analysis prompt + robustness gauntlet; analysis cache.
- **M2 — Viewer**: embedded HTML template; GitHub-fidelity rendering (Primer
  tokens, fonts, chroma syntax highlighting); unified + split views;
  word-level intra-line highlights; unmodified-region folding; step nav,
  overview + diagram, collapsible files; dependency chips; per-hunk blame
  (local-git sources).
- **M3 — Scale + ship**: staged analysis for huge diffs; `--mock`/fixture
  mode for viewer development; goreleaser + GitHub releases; README, LICENSE
  (MIT), CONTRIBUTING with the provider how-to; repo goes public.

## Testing & validation policy *(standing rule, locked 2026-07-16)*

Applies to every gate; every gate handoff MUST carry this into its spec:

1. **Each handoff includes a "How to test/validate" section** with exact
   commands and expected outcomes — an implementing agent (and the reviewer)
   must be able to verify the gate without reverse-engineering intent.
2. **Unit tests** for every non-trivial pure component (parsers, repair,
   validation, config merging, cache keys, fingerprints).
3. **End-to-end tests ship in the repo** (`test/e2e`, plain `go test`): they
   build the actual binary and run it as a subprocess against fixtures,
   asserting on real outputs, exit codes, and stderr behavior. Deterministic
   by default (mock provider — no network, no LLM spend, CI-safe).
4. **Real-provider e2e** exists but is opt-in via env var
   (e.g. `BGR_E2E_CLAUDE=1`), skipping cleanly when unset or the provider is
   unavailable. Never runs in CI by default.
5. **A gate is not reviewable** unless `go build ./...`, `go vet ./...`, and
   `go test ./...` (including e2e) pass from a clean checkout.

## Next step

Product decisions #1–14 and both design contracts are locked
(reviewed 2026-07-16). The plan is build-ready: M1 starts on approval.

Remaining small calls, deliberately left for build time: viewer details
(viewed checkboxes, diagram renderer, dark/light behavior), staged-analysis
call-count threshold default, cache eviction policy.
