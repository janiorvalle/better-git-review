# pr-walkthrough

Turn a PR or branch diff into a **guided HTML walkthrough**: changes are clustered
into intent-based cohorts, ordered logically (schema → backend → API → UI → tests),
and presented as a step-by-step viewer with scoped summaries, rendered diffs, and a
relationship diagram. Inspired by CodeRabbit's Change Stack.

Zero npm dependencies. Analysis runs through your existing Claude Code install
(`claude -p` headless mode), so there's no API key to manage.

## Requirements

- Node 18+
- [`claude`](https://claude.com/claude-code) CLI, logged in (for analysis)
- [`gh`](https://cli.github.com) CLI, logged in (only for `--pr` mode)
- `git` (only for branch mode)

## Install

```sh
cd pr-walkthrough
npm link        # puts `pr-walkthrough` on your PATH
```

Or just run it directly: `node cli.js ...`

## Usage

```sh
# From inside a repo: walk through the current branch vs main
pr-walkthrough

# A GitHub PR (uses gh for the diff + title/description)
pr-walkthrough 1234

# Another repo, explicit base branch
pr-walkthrough -C ~/code/my-repo --base origin/develop

# A patch file
pr-walkthrough --diff changes.patch

# Options
pr-walkthrough --model opus        # default: sonnet
pr-walkthrough --out review.html   # default: ./walkthrough-<name>.html
pr-walkthrough --no-open           # don't auto-open the result
pr-walkthrough --mock              # skip the LLM, group by path heuristics (dev/testing)
```

The output is a single self-contained HTML file — open it anywhere, share it, attach
it to a review. (The mermaid diagram loads its renderer from a CDN; offline you'll
see the diagram source instead.)

## How it works

1. **Collect** — grabs the diff via `gh pr diff`, `git diff base...HEAD`, or a patch file.
2. **Parse** — splits the unified diff into files and hunks with line numbers.
3. **Analyze** — sends the annotated diff to `claude -p`, which returns JSON:
   an overview, a mermaid relationship diagram, and ordered cohorts, each with an
   intent, a reviewer narrative, per-file summaries, and review notes.
4. **Render** — embeds everything into a static HTML template with a step-by-step
   viewer (sidebar nav, keyboard ←/→, collapsible diffs, dark mode).

Very large diffs are capped (~160k chars total, ~12k per file) before analysis;
truncated files are marked in the prompt. Files the model fails to assign land in
an "Other changes" cohort so nothing silently disappears.
