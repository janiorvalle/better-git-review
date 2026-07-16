# better-git-review

Turn any diff into a **guided HTML walkthrough**: changes clustered into
intent-based cohorts, ordered the way a reviewer should read them
(schema → backend → API → UI → tests), rendered with GitHub-fidelity diffs —
as a single self-contained HTML file you can open anywhere.

**Status: in development.** See [PLAN.md](PLAN.md) for the locked design.
A working Node prototype lives in [`prototype/`](prototype/) and serves as
the behavioral reference for the Go implementation.

- Forge-agnostic: local git refs and patch files are first-class; GitHub (and
  eventually other forges) are optional convenience adapters.
- Provider-agnostic: pluggable LLM backends (claude CLI, codex CLI,
  OpenRouter in v1) behind a three-method interface.
- One binary, one output file, MIT licensed.
