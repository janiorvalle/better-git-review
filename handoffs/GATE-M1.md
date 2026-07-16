# Gate M1 — Core Pipeline (handoff)

You are implementing Gate M1 of `better-git-review`. This document is
self-contained, but the authoritative design is `PLAN.md` at the repo root —
read it first. A working Node prototype in `prototype/` shows the intended
end-to-end behavior (its analysis quality is the bar to meet). Where this
document and PLAN.md conflict, PLAN.md wins; note the conflict in your PR.

## What the product is

A Go CLI that turns a diff (GitHub PR, local git refs, or patch file) into a
guided review walkthrough: an LLM clusters the changed files into intent-based
cohorts ordered for a reviewer (schema → backend → API → UI → tests). Gate M2
adds the HTML viewer. **Your gate produces the machine-readable walkthrough
document** — everything up to but excluding rendering.

## Process requirements

- Branch: `gate/m1-core-pipeline` off `main`. Open a PR to `main` when done.
  **Do not merge it** — it will be reviewed.
- Do not modify `PLAN.md`, `prototype/`, `reference/`, or `handoffs/`.
- If something is ambiguous, make a reasonable choice and document it in the
  PR description under a "Deviations & decisions" heading. Do not stall.
- Commit in coherent increments, not one giant commit.

## Deliverables

Go module `github.com/janiorvalle/better-git-review`, binary
`better-git-review` (built from `cmd/better-git-review`). Suggested internal
layout (not mandated): `internal/{diff,source,provider,config,analyze}`.

**Dependency policy:** standard library plus `github.com/BurntSushi/toml`
only. No JSON-schema library (validation is plain Go code), no CLI framework
(stdlib `flag` is fine), no HTTP client libs.

### 1. CLI surface (M1 scope)

```
better-git-review [PR_NUMBER] [flags]

  PR_NUMBER          GitHub PR via `gh` (adapter; needs gh installed+authed)
  --diff <file|->    unified diff from a patch file or stdin
  --base <ref>       diff <ref>...HEAD in the repo (default source when no
                     PR/diff given; auto-detect base: origin/HEAD symbolic
                     ref, then origin/main, origin/master, main, master)
  -C <dir>           repo directory (default: cwd)
  --provider <name>  force a provider (else: config, else auto-detect)
  --model <m>        model override for the chosen provider
  --out <file>       output path (default: walkthrough-<name>.json)
  --no-cache         bypass the analysis cache
  --yes              skip interactive confirmations (cost guard, trust prompt)
  --trust-repo-config  accept the current repo config fingerprint (see TOFU)
```

Behavioral details proven out in the prototype (`prototype/cli.js`) — match
them: empty `base...HEAD` falls back to `git diff HEAD` (uncommitted changes);
PR mode pulls title/body via `gh pr view --json` for analysis context;
`git diff` / `gh` are shelled out to, not reimplemented.

### 2. Output document — the M1→M2 contract

A single JSON file. This schema is load-bearing (M2 renders from it); changes
require a note in the PR.

```jsonc
{
  "schemaVersion": 1,
  "source": {
    "title": "PR #4569: ...", "description": "...", "range": "main ← feature",
    "url": "https://... or null", "name": "slug-for-filenames",
    "repoDir": "/abs/path or empty (patch mode)"
  },
  "files": [{
    "path": "src/a.go", "oldPath": "...", "newPath": "...",
    "status": "modified|added|deleted|renamed", "binary": false,
    "additions": 10, "deletions": 2,
    "hunks": [{ "header": "func foo()", "lines": [
      { "t": "a|d|c", "old": 0, "new": 42, "text": "..." }  // 0 = no line no.
    ]}]
  }],
  "analysis": {
    "title": "...", "overview": "...", "mermaid": "graph LR ... or null",
    "cohorts": [{
      "title": "...", "layer": "schema|backend|api|ui|tests|config|docs|other",
      "intent": "...", "narrative": "...",
      "files": [0, 2],                    // indexes into files[]
      "fileSummaries": ["...", "..."],    // parallel to files
      "reviewNotes": ["..."],
      "dependsOn": [0]                    // indexes of EARLIER cohorts only
    }]
  },
  "meta": { "provider": "claude-cli", "model": "sonnet",
            "generator": "better-git-review v0.x", "cached": false }
}
```

### 3. Diff parser

Port `parseDiff` from `prototype/cli.js` to Go. Must handle: added/deleted/
renamed files, binary files, quoted paths, hunk headers with context,
`\ No newline at end of file`, mode-change-only entries. Unit-test with
fixture patches covering each case.

### 4. Provider layer (PLAN.md "Provider interface contract" — locked)

```go
type Provider interface {
    Name() string
    Detect() (available bool, detail string)
    Complete(ctx context.Context, prompt string) (string, error)
}

type StructuredProvider interface {
    Provider
    CompleteStructured(ctx context.Context, prompt string, schema json.RawMessage) (json.RawMessage, error)
}
```

Providers to ship:

- **claude-cli** — shell out to `claude -p --model <m> --output-format json`,
  prompt via stdin. CRITICAL, learned in prototype testing: the `json` output
  format varies by CLI version — a single result object, an ARRAY of events
  (find the `"type":"result"` element; respect its `is_error`), or a bare
  JSON-encoded string. Handle all three (see `callClaude` in
  `prototype/cli.js`).
- **codex-cli** — shell out to `codex`. Verify the invocation against the
  installed CLI's `--help` (e.g. `codex exec` with a flag to emit the last
  message); document what you built against.
- **openrouter** — direct HTTP to `<base_url>/chat/completions` (default
  `https://openrouter.ai/api/v1`), stdlib `net/http`. Implements
  `StructuredProvider` via `response_format: {type: "json_schema", ...}`
  (strict). API key read from the env var NAMED in config
  (default name `OPENROUTER_API_KEY`) — never from config directly.
- **mock** — hidden from detection; selected only by explicit
  `--provider mock`. Returns a deterministic canned analysis derived from the
  file list (port the prototype's `mockAnalysis` path heuristics). Exists for
  tests and for M2 viewer development.

Selection: explicit flag > config > auto-detect in order
`claude-cli, codex-cli, openrouter(key present)`. Always announce
`provider/model` on stderr before any call. No provider found → exit with an
error listing what was probed and how to configure.

### 5. Config + trust (locked design — see PLAN.md)

- User config `~/.config/better-git-review/config.toml`; repo config
  `.better-git-review.toml` at repo root; precedence flags > repo > user.
- TOML shape:

```toml
provider = "openrouter"
[providers.openrouter]
model = "anthropic/claude-sonnet-4-5"
api_key_env = "OPENROUTER_API_KEY"
base_url = ""   # optional override
[providers.claude-cli]
model = "sonnet"
```

- **Trust-on-first-use for repo config**: if repo config sets/changes any
  provider-relevant field (provider selection, any `[providers.*]` content),
  compute a fingerprint (sha256 over a canonical serialization of those
  fields). Compare against the stored fingerprint for this repo path in
  `~/.config/better-git-review/trust.toml`. On first sight or change: show
  exactly what the repo config wants and confirm interactively (or accept via
  `--trust-repo-config` / `--yes`); store on approval. Non-TTY without an
  accept flag: refuse with instructions. Rationale: a malicious cloned repo
  must not silently redirect diffs to an attacker endpoint.

### 6. Analysis + robustness gauntlet (locked design — see PLAN.md)

Build the analysis prompt from the parsed diff (adapt the prototype's
`buildPrompt`: per-file diff text with caps — ~160k chars total budget,
per-file cap; mark truncations). Add the `dependsOn` field to the requested
schema (new vs. prototype).

Every plain-text provider response runs the gauntlet:
1. Extract JSON (strip fences/prose).
2. Parse; on failure, repair pass (escape unescaped quotes and raw newlines
   inside strings — port `repairJson` from the prototype; keep its unit test
   cases and add more).
3. Validate in code: cohorts non-empty; layer in enum; file indexes in range;
   `fileSummaries` parallel to `files`; `dependsOn` references strictly
   earlier cohort indexes.
4. On parse/validation failure: ONE retry, re-prompting with the exact
   validation errors quoted back. Max 2 model calls total for the analysis.
5. Deterministic seatbelts in code (never trust the model): every file in
   exactly one cohort — strays appended to an "Other changes" cohort; invalid
   layer → "other"; invalid dependsOn entries dropped; empty cohorts removed.
6. Still failing → exit nonzero; write the raw model output to
   `~/.local/state/better-git-review/debug-<timestamp>.txt` and print that
   path in the error. Never emit a partial document.

`StructuredProvider` path: skip steps 1–2, still run 3–5 (schema-shape is
guaranteed; semantics are not).

### 7. Cache (locked design)

Key: sha256 over (diff bytes, provider name, model, schemaVersion). Store the
final document JSON under `~/.local/state/better-git-review/cache/<key>.json`.
Hit → reuse with `meta.cached=true` (log "cache hit" on stderr); `--no-cache`
bypasses. No eviction policy required in M1.

### 8. Cost guard (plumbing only in M1)

Count planned provider calls; if the plan exceeds 5 calls, print the plan
(N calls, provider, model) and require confirmation or `--yes`. In M1
single-pass mode this effectively never triggers (staged analysis is M3), but
the accounting and confirmation path must exist and be tested.

## How to test/validate (required deliverables, per PLAN.md testing policy)

### Unit tests (alongside the code they test)

- Diff parser: fixture patches covering add/delete/rename/binary/quoted
  paths/no-newline/mode-only entries.
- Gauntlet: repairJson cases (unescaped quotes, raw newlines — port the
  prototype's cases and extend), extraction (fenced/prose-wrapped/bare),
  validation failures, every seatbelt (stray files, bad layer, bad dependsOn).
- claude-cli output parsing: canned stdout samples for all three shapes
  (object / event array / bare string) — never call the real CLI in unit tests.
- Config: precedence (flags > repo > user), TOFU fingerprint stability and
  change detection.
- Cache: key derivation (each input changes the key), roundtrip, corrupt
  cache entry → treated as miss, not a crash.

### E2E tests (`test/e2e`, plain `go test ./test/e2e/...`)

Build the real binary once (TestMain → `go build` into t.TempDir()), then run
it as a subprocess. Deterministic, no network, CI-safe — use
`--provider mock` and fixture patches. Required scenarios:

1. **Happy path**: `--diff fixture.patch --provider mock --out out.json`
   → exit 0; out.json parses; schema rules from §2 hold (files partitioned
   into cohorts, layers in enum, dependsOn references earlier cohorts only);
   `meta.cached == false`.
2. **Cache**: same command again → exit 0, `meta.cached == true`, stderr
   mentions cache hit; with `--no-cache` → `meta.cached == false`.
3. **Stdin**: `--diff -` with the patch on stdin → same as (1).
4. **Git source**: create a throwaway git repo in t.TempDir() (init, commit,
   branch, commit), run with `--base` → document reflects the branch diff;
   also cover the empty-branch → uncommitted-changes fallback.
5. **Failure modes**: empty diff → nonzero exit + clear message; no provider
   available (PATH stripped, no config) → nonzero exit, error lists probes;
   unreadable `--diff` path → nonzero exit.
6. **Trust prompt**: repo with a `.better-git-review.toml` that sets a
   provider, non-TTY, no accept flag → refuses with instructions; with
   `--trust-repo-config` → proceeds and a second run no longer prompts.

### Real-provider e2e (opt-in, same package)

Gated on `BGR_E2E_CLAUDE=1` AND `claude` being on PATH — otherwise
`t.Skip` with a clear message. One scenario: real diff fixture through
claude-cli end-to-end, assert the document validates. Never runs in CI or by
default. Do NOT gate any required functionality on this test.

### Validation commands (what the reviewer will run from a clean checkout)

```sh
go build ./...
go vet ./...
go test ./...                 # includes test/e2e, must pass with no network
BGR_E2E_CLAUDE=1 go test ./test/e2e/ -run Claude -v   # if claude available
```

## Out of scope for this gate — do NOT build

HTML/viewer anything (M2), syntax highlighting, blame, staged analysis for
huge diffs (M3), GitLab or other forge adapters, goreleaser/release tooling,
streaming, Homebrew.

## Acceptance checklist (the review will check exactly this)

- [ ] `go build ./...`, `go vet ./...`, `go test ./...` all clean from a
      clean checkout, no network required
- [ ] Only allowed dependency in go.mod: BurntSushi/toml
- [ ] All unit tests from "How to test/validate" present and passing
- [ ] All six e2e scenarios from "How to test/validate" present and passing
      (`test/e2e`, subprocess against the built binary, mock provider)
- [ ] Opt-in real-provider e2e present, skips cleanly when ungated
- [ ] A real end-to-end run against a real repo diff with claude-cli,
      output attached or quoted in the PR description
- [ ] PR to `main` from `gate/m1-core-pipeline`, left unmerged, with a
      "Deviations & decisions" section
