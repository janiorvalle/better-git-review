# Gate M3 — Scale + Ship (handoff)

You are implementing Gate M3, the final v1 gate of `better-git-review`.
Read `PLAN.md` first — it is the design authority. Gates M1 (core pipeline)
and M2 (viewer) are merged. **Your gate makes huge diffs work (staged
analysis), hardens the prompt boundary, and makes the project releasable:
CI, release tooling, and public-quality docs.**

## Precondition

`main` must contain merged M1 + M2. Branch `gate/m3-scale-ship` off `main`.
PR to `main`; leave unmerged. Do not modify `PLAN.md`, `prototype/`,
`reference/`, or `handoffs/`. Document deviations in the PR under
"Deviations & decisions". Commit incrementally.

## Deliverables

### 1. Staged analysis for huge diffs (locked, PLAN #8)

When the single-pass prompt would exceed its budget (the existing ~160k-char
diff cap), switch to two stages:

- **Stage 1 — per-file summaries.** One provider call per file (or per small
  directory group for many tiny files — your judgment), each producing a
  strict-JSON summary: `{ "summary": "...", "layerHint": "...",
  "keySymbols": ["..."] }`. Each call runs the existing gauntlet (extract →
  repair → validate → one retry). Bounded concurrency (default 4 concurrent
  provider calls; a package-level constant is fine).
- **Stage 2 — clustering.** One call that receives the file list with
  stage-1 summaries (not raw diffs) and produces the normal analysis JSON,
  through the normal gauntlet. Hard fail per the locked robustness contract
  if this call fails after retry — no heuristic fallback.
- **Degradation:** a file whose summary fails after retry enters stage 2
  with a path-derived stub summary and is flagged (see schema below). The
  run continues.
- Full diffs still render in the HTML regardless — staging affects only the
  analysis input.
- Testability requirement: the staging threshold must be overridable via env
  var `BGR_STAGE_BUDGET` (bytes) so e2e can force staging on small fixtures.
  Undocumented in --help; document in code.

**Schema changes (bump `schemaVersion` to 3):**
- `meta.staged` (bool) — whether staged analysis ran.
- `analysis.stubbedFiles` ([]int, may be empty) — file indexes whose
  summaries are path-derived stubs. The viewer must visibly flag these files
  ("no model summary — grouped from path only" or similar).

### 2. Cost guard goes live (locked design)

The guard exists; staging makes it real. The plan must count stage-1 calls +
1 clustering call (+ worst-case retries stated in the message). Over the
threshold (5): print the plan (N calls, provider, model) and require
interactive confirm or `--yes`; non-TTY without `--yes` refuses with
instructions. This flow needs real e2e coverage now (see tests).

### 3. Prompt-boundary hardening (carried from Gate M1 review)

The untrusted-data framing currently uses static delimiters
(`BEGIN_UNTRUSTED_CHANGE_DATA`/`END_...`). A hostile diff containing the END
marker can partially escape the framing. Fix: per-run random delimiters
(e.g. `BEGIN_UNTRUSTED_<16-hex-random>`), and strip/neutralize any occurrence
of the chosen delimiter inside the framed content before assembly. Applies
to both single-pass and staged prompts. Unit-test with a diff that contains
the delimiter text.

### 4. CI (GitHub Actions)

`.github/workflows/ci.yml`: on PR and push to main — `go build ./...`,
`go vet ./...`, `go test ./...` on ubuntu + macos, Go stable. Must pass with
no network beyond module download (the deterministic test suite already
guarantees this). Real-provider e2e stays opt-in and does NOT run in CI.

### 5. Release tooling (locked, PLAN #12)

- `.goreleaser.yaml`: archives for darwin/linux/windows, amd64+arm64,
  binary `better-git-review`, checksums file. Version injected via ldflags
  into the existing generator/version string (`--version` flag prints it —
  add the flag if absent).
- `.github/workflows/release.yml`: on tag `v*`, run tests, then goreleaser
  to GitHub Releases.
- Do NOT create or push any tag, and do NOT publish a release — tooling
  only. Validate config with goreleaser's check/snapshot mode if available
  locally; otherwise document that validation was config-review only.

### 6. Public-quality docs

- **README.md** (rewrite): what it is (one screenshot-worthy paragraph +
  the walkthrough concept), install (GitHub releases; `go install` works
  too), quickstart for all three sources, provider setup for claude-cli /
  codex-cli / openrouter, config file reference, cache/trust/cost-guard
  behavior notes, huge-diff staging note.
- **CONTRIBUTING.md**: dev setup, test policy summary (unit + e2e + opt-in
  real-provider), and a "writing a provider" walkthrough — the three-method
  interface, the optional StructuredProvider upgrade, detection etiquette,
  and what the core guarantees so providers stay ~50 lines.
- Do NOT change LICENSE. Do NOT make the repo public — owner's call, out of
  scope.

## How to test/validate (per PLAN.md testing policy)

### Unit tests

- Staging trigger math (under/at/over budget; BGR_STAGE_BUDGET override).
- Stage-1 summary gauntlet (bad JSON → retry → stub degradation path).
- Cost-guard plan arithmetic (files + cluster + retry ceiling).
- Nonce delimiter generation + neutralization of embedded delimiter text.
- Concurrency: stage-1 respects the bound (fake provider recording
  concurrent-call high-water mark).

### E2E tests (extend `test/e2e`)

1. **Staged happy path**: multi-file fixture + `BGR_STAGE_BUDGET` forcing
   staging + mock provider → exit 0, `meta.staged == true`, document valid,
   HTML renders with all files.
2. **Stub degradation**: mock provider rigged to fail one file's summary
   (mock needs a failure hook — env var or magic filename is fine) →
   run succeeds, file listed in `analysis.stubbedFiles`, HTML flags it.
3. **Cost guard**: staged run over threshold, non-TTY, no `--yes` → nonzero
   exit, message names call count and `--yes`; same with `--yes` → proceeds.
4. **Schema v3**: v2 cache entries not reused; `--format json` documents
   carry `meta.staged` and `stubbedFiles`.
5. **Delimiter injection**: fixture diff containing the static delimiter
   text and instruction-like content → prompt assembly neutralizes it (assert
   via a mock provider that records the received prompt).
6. **Single-pass regression**: small diff still takes the single-pass path
   (`meta.staged == false`) — staging must not regress the common case.

### Real-provider e2e (opt-in, unchanged)

`BGR_E2E_CLAUDE=1` run still passes (single-pass). Optionally add a gated
staged real run behind `BGR_E2E_CLAUDE_STAGED=1`; not required.

### Validation commands

```sh
go build ./... && go vet ./... && go test ./...
BGR_E2E_CLAUDE=1 go test ./test/e2e/ -run Claude -v -count=1
goreleaser check          # or document config-review if unavailable
```

Include in the PR: output of a real staged run against a genuinely large
diff (hundreds of files — e.g. generate one from a big repo locally),
with timing, call count, and the resulting cohort list quoted.

## Out of scope — do NOT build

Making the repo public, publishing releases or tags, Homebrew/go-install
distribution docs beyond the README lines above, GitLab adapters, `--serve`,
viewed checkboxes, semantic delta labels, streaming.

## Acceptance checklist (the review will check exactly this)

- [ ] Build/vet/test clean from clean checkout, no network
- [ ] Staged analysis: trigger, bounded concurrency, per-call gauntlet,
      stub degradation, `meta.staged`/`stubbedFiles` in schema v3, viewer
      flags stubbed files
- [ ] Cost guard live with real e2e coverage (refuse / --yes paths)
- [ ] Nonce delimiters + neutralization, unit- and e2e-tested
- [ ] CI workflow present and green on the PR
- [ ] goreleaser config + release workflow present; no tags/releases created
- [ ] README + CONTRIBUTING at public quality (provider how-to included)
- [ ] All six e2e scenarios above present and passing; existing suites
      still green; opt-in Claude e2e passes
- [ ] Real staged-run proof on a large diff in the PR description
- [ ] PR from `gate/m3-scale-ship`, unmerged, with "Deviations & decisions"
