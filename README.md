# better-git-review

<p align="center">
  <img src="assets/hero.png" alt="bgr — better-git-review. git reviews, but better. obviously." width="840">
</p>

You open a 40-file PR and the diff is sorted alphabetically. The migration
is at the bottom, the API change that depends on it is at the top, and the
one line that actually matters is folded somewhere in the middle.

`bgr` reads the diff with an LLM and hands you a guided walkthrough instead:
related changes grouped into ordered steps, each with a plain-language
narrative, the risks worth double-checking, and the full diff rendered the
way GitHub renders it — syntax highlighting, unified/split views, word-level
changes, folding, blame. The whole thing is one HTML file. Open it anywhere,
attach it to the PR, send it to a teammate. No server, no account, nothing
leaves your machine except the diff you send to the model you chose.

## Install

macOS or Linux — the installer verifies checksums and writes to
`~/.local/bin`, no sudo:

```sh
curl -fsSL https://raw.githubusercontent.com/janiorvalle/better-git-review/main/install.sh | sh
```

Windows — grab the zip from
[Releases](https://github.com/janiorvalle/better-git-review/releases) and put
`bgr.exe` on your `PATH`.

Go users:

```sh
go install github.com/janiorvalle/better-git-review/cmd/bgr@latest
```

Archives ship both `bgr` and the `better-git-review` long-name alias. Check
with `bgr --version`. The first interactive run walks you through picking a
provider; `bgr configure` changes it later.

## Why

<p align="center">
  <img src="assets/review-by-intent.png" alt="alphabetical order is not a review strategy" width="840">
</p>

<p align="center">
  <img src="assets/no-skeletons.png" alt="no skeletons. no spinners. it's just a file." width="840"></p>

[Open a generated example walkthrough](docs/example-walkthrough.html) — bgr
reviewing one of its own pull requests.

## Use it

Review your branch against main:

```sh
bgr
```

Review before you commit, one commit, or any two refs:

```sh
bgr --dirty
bgr --commit HEAD
bgr --base main --head feature
```

Not sure what to review? Pick from your PRs, branches, and recent commits:

```sh
bgr -i
```

A GitHub PR, via the authenticated `gh` CLI:

```sh
bgr 123
```

GitHub is optional — PR-by-number is the only mode that touches it. Refs,
commits, dirty trees, patch files, and stdin are forge-agnostic:

```sh
bgr --diff change.patch
git diff main...HEAD | bgr --diff -
```

HTML is the default output and opens in your browser automatically when
you're in a terminal (`--no-open` to skip, `--open` to force it from a
script). `--out review.html` picks the path. `--format json` gives you the
schema-versioned analysis document instead — that's the machine interface,
and it's what agents should read.

## Providers

`bgr` doesn't bundle a model. It uses what you already have, detected in
this order: `claude-cli`, `codex-cli`, `openrouter`.

- **Claude CLI** — rides your existing `claude` login. Default model
  `sonnet`. Runs with tools and session persistence disabled.
- **Codex CLI** — rides `codex`. Default `gpt-5.6-luna` at `low` reasoning;
  override with `--model` and `--reasoning`. Runs in an isolated read-only
  workspace with host, network, browser, plugin, and shell tools disabled —
  the diff goes in through the prompt and nothing else.
- **OpenRouter** — set `OPENROUTER_API_KEY` and you're on `z-ai/glm-5.2` by
  default, with structured JSON output. Any model on the catalog works.

Writing a new provider is a three-method interface and ~50 lines — see
[CONTRIBUTING.md](CONTRIBUTING.md).

## Configuration

User config lives at `~/.config/better-git-review/config.toml` on macOS and
Linux, `%APPDATA%\better-git-review\config.toml` on Windows. Repos can add a
`.better-git-review.toml`; precedence is flags > repo > user.

```toml
provider = "openrouter"
auto_open = true
include_mechanical = false

[providers.openrouter]
model = "z-ai/glm-5.2"
api_key_env = "OPENROUTER_API_KEY"

[providers.claude-cli]
model = "sonnet"

[providers.codex-cli]
model = "gpt-5.6-luna"
reasoning = "low"

[analysis]
summary_batch_max_files = 25
stage_concurrency = 4
digest_max_files = 40
digest_max_chars = 60000
file_diff_cap = 12000
guard_call_threshold = 5
staging_max_files = 150
fidelity_budget = 4000000

[viewer]
collapse_threshold = 400
fold_threshold = 10
fold_context = 3
long_line_threshold = 4096
key_symbol_cap = 5
word_diff_min_similarity = 0.5
theme_light = "github"
theme_dark = "github-dark"

[media]
max_preview_bytes = 1572864
max_total_preview_bytes = 12582912
image_extensions = [".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg"]

[git]
context_lines = 0
find_renames = 0

[github]
pr_diff_max_files = 300
list_limit = 1000

[network]
catalog_timeout_seconds = 10
completion_timeout_seconds = 300
provider_exec_timeout_seconds = 600

[cache]
max_entries = 200

[browser]
command = ""
```

Never put an API key in config — `api_key_env` names the environment
variable that holds it.

All values above are the defaults. `git.context_lines = 0` and
`git.find_renames = 0` leave Git's defaults alone. `cache.max_entries = 0`
disables eviction. Browser resolution is `browser.command`, then `$BROWSER`,
then the platform default.

Repo config may set only `auto_open`, `[viewer]`, and `[media]` without a
trust prompt. Other tables above are user-config only and are ignored with a
warning when found in repo config. Repo media byte limits may tighten, but
cannot raise, the user's limits. Existing repo provider, endpoint, key
variable, or mechanical-file settings remain trust-gated: you'll see exactly
what the repo wants and confirm once; if those settings change later, you'll
be asked again. A cloned repo shouldn't get to silently redirect your diffs
or expand model spend.
Non-interactive runs pass `--trust-repo-config` or `--yes`.

Some invariants are deliberately not configurable: prompt-injection
delimiters; Claude/Codex subprocess isolation flags (`--safe-mode`,
`--sandbox`, and disabled features); `SchemaVersion`; the review-layer list;
cache-key composition; and trust-file mechanics. These protect artifact
compatibility and the security boundary rather than tune behavior.

## Cost, caching, and big diffs

Analysis is cached on the diff content, provider, model, reasoning level,
analysis budget, and mechanical-file selection — re-running an unchanged
diff is instant and free. `--no-cache` forces a fresh pass.

Diffs of up to 150 files are one model call when they fit the selected
model's input budget. Anything larger by characters or file count gets staged:
provably mechanical files (exact renames, repository-attested generated
files, and binaries) are kept in the walkthrough without spending model
calls; the rest are summarized in bounded batches. Go assigns every file
to deterministic directory cohorts, each cohort gets one bounded
narration, and one final call synthesizes the overview. `--include-mechanical`
or `include_mechanical = true` opts every file back into model analysis.
The HTML always carries the complete diffs either way.

Any plan over five calls tells you the exact planned count — the same
immutable batch/cohort plan the executor uses — and asks first. A failed
summary batch retries once, then every file in that batch is visibly
stubbed without changing the remaining schedule. Scripts pass `--yes`.

## Agents

`bgr` ships an agent skill that teaches Claude Code or Codex when and how
to use it (`--format json`, cohort-by-cohort review, the etiquette). Install
it from `bgr configure` — it's opt-in, never automatic.

## Development

[CONTRIBUTING.md](CONTRIBUTING.md) has setup, the test policy, and the
provider and source-adapter contracts. [SECURITY.md](SECURITY.md) has the
disclosure process and trust model.

## License

MIT. See [LICENSE](LICENSE).
