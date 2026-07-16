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

*(more items land here as they arrive)*
