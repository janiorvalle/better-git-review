---
name: bgr
description: Turn a pull request, commit, branch diff, or working tree into a structured review walkthrough.
---

# bgr

> Minimal functional skill. Content pending the docs voice pass.

Use `bgr` when a user asks to review a PR, diff, branch, commit, or local changes.

- Use `bgr <PR_NUMBER>` for a GitHub pull request.
- Use `bgr --commit <sha>` for one commit, including "review my last commit".
- Use `bgr --dirty` before committing local work.
- Use `bgr --base <ref> --head <ref>` to compare arbitrary refs.
- Use `bgr --format json` when the analysis itself is input to another tool.
- Never use `bgr -i` from a non-interactive agent run.

Read `analysis.cohorts` in order. Use `dependsOn` to understand prerequisites and `reviewNotes` to focus the review. Pass `--yes` for approved non-interactive staged analyses. Do not use `--no-cache` without a concrete reason; cached analysis avoids repeated cost.
