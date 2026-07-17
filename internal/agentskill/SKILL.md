---
name: bgr
description: Turn a pull request, commit, branch diff, or working tree into a structured review walkthrough. Use when asked to review code changes, and read the JSON output as a ready-made review plan.
---

# bgr

`bgr` analyzes a diff with an LLM and produces a guided review walkthrough:
related files grouped into ordered cohorts, each with an intent, a narrative,
and specific review notes. Reach for it when a user asks you to review a PR,
a commit, a branch, or uncommitted work — the analysis gives you a review
structure instead of a flat wall of files.

## Picking the right source

- `bgr 123` — a GitHub PR (needs an authenticated `gh`; falls back to local
  git objects when the API won't serve the diff).
- `bgr --commit <sha>` — exactly one commit. "Review my last commit" is
  `bgr --commit HEAD`.
- `bgr --dirty` — uncommitted changes only. The right pre-commit self-review.
- `bgr --base <ref> --head <ref>` — any two refs; `--head` defaults to HEAD.
- `bgr --diff file.patch` or `git diff ... | bgr --diff -` — any unified
  diff, no forge or git required.
- No GitHub? Everything except `bgr <PR#>` works without `gh` installed.

## Agent etiquette

- Use `--format json --out <path>` and read the document. HTML is for
  humans; stderr progress text is not a stable interface — never parse it.
- Pass `--yes` on any run that might stage (large diffs trigger a cost
  guard above 5 provider calls, and a non-TTY run without `--yes` fails).
- Never use `-i` (the interactive picker) or `bgr configure` from an agent
  run; both are interactive. Edit config files directly instead.
- Don't pass `--no-cache` without a concrete reason. Analysis is cached on
  the diff, provider/model/reasoning, budget, and mechanical selection —
  re-runs of an unchanged diff are free.
- Auto-open only happens in a TTY, so agent runs won't pop browsers. If a
  human asked for the walkthrough, tell them the `--out` path.

## Reading the document (`--format json`)

- `schemaVersion` — currently 5; validate before depending on fields.
- `analysis.cohorts[]` — ordered for review. Walk them in order. Each has
  `title`, `layer` (schema|backend|api|ui|tests|config|docs|other),
  `intent`, `narrative`, `reviewNotes[]` (specific risks worth checking —
  use these to focus), `files[]` (indexes into the top-level `files[]`),
  and `dependsOn[]` (indexes of earlier cohorts this builds on).
- `files[]` — the full parsed diff: paths, status, hunks with line numbers.
- `analysis.stubbedFiles[]` — files whose per-file summary failed during
  staged analysis; their grouping is path-derived, so trust it less.
- `analysis.mechanicalFiles[]` — exact renames, repository-attested
  generated files, and binaries deliberately skipped by the model. These
  are neutral provenance, not analysis failures.
- `meta.staged` — true when the diff was too large for one pass and was
  triaged, summarized in bounded batches, and grouped deterministically.

A solid review workflow: run with `--format json`, walk cohorts in order,
verify each cohort's `reviewNotes` against the actual hunks in `files`, and
report findings grouped by cohort.

## Configuration

- User config: `~/.config/better-git-review/config.toml` on macOS/Linux,
  `%APPDATA%\better-git-review\config.toml` on Windows.
- Repo config: `.better-git-review.toml` at the repo root. Precedence is
  flags > repo > user.
- Provider blocks take `model` and `reasoning`; keys are named by env var,
  never stored:

  ```toml
  provider = "openrouter"
  include_mechanical = false

  [providers.openrouter]
  model = "z-ai/glm-5.2"
  api_key_env = "OPENROUTER_API_KEY"

  [providers.claude-cli]
  model = "sonnet"

  [providers.codex-cli]
  model = "gpt-5.6-luna"
  reasoning = "low"
  ```

- **Trust gotcha:** repo config that sets provider fields is fingerprinted
  and requires trust on first use. If you edit a repo's
  `.better-git-review.toml` provider settings, the next run will prompt —
  or fail in non-TTY. Pass `--trust-repo-config` (or `--yes`) on that next
  run, and say so in your report.

## Providers

Auto-detected in order: `claude-cli`, `codex-cli`, `openrouter` (when its
key env var is set). Override with `--provider`, `--model`, `--reasoning`.
Defaults: claude-cli → `sonnet`; codex-cli → `gpt-5.6-luna` at `low`
reasoning; openrouter → `z-ai/glm-5.2`. `--provider mock` is deterministic
and free — use it when testing anything around bgr rather than spending
provider calls.

## Failure modes worth knowing

- Empty diff → nonzero exit, "nothing to review".
- PR mode without `gh` → nonzero exit; the message lists the ref-based
  alternatives above.
- Analysis that fails validation twice → nonzero exit, raw model output
  saved to a debug file (path is in the error).
- Blame strips only appear for local-git sources; patch mode has none.
