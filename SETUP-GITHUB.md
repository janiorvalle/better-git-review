# bgr public-flip requirements (settings-level)

Adapted from drawover's `SETUP-GITHUB.local.md`. Everything in-code
(workflows, gitleaks, Scorecard, CLA action, SECURITY.md, attestation)
already landed in Gate M4.

**Owner decision (2026-07-16): these settings are applied via Terraform,
not by hand.** A separate PRIVATE repo (working name
`janiorvalle/github-baseline`) holds an `oss-baseline` module using the
GitHub provider, instantiated for `better-git-review` AND `drawoverlay`
(imported, so the module is validated against the already-trusted repo).
Fine-grained admin PAT via env var only; gitignored local state
(recoverable via import); scheduled `terraform plan` as drift detection.
This document is therefore the module's REQUIREMENTS SPEC plus the few
non-Terraform items (CLA signatures branch, pre-public gitleaks history
scan, artifacts decision, npm-side bindings for drawover).

Endgame order: **M6 merged → design PR merged → docs voice pass →
baseline repo built + applied → tag v1.1.0 → flip public.**

## Before flipping public

- [ ] `make verify` green from a clean local `main`.
- [ ] `gitleaks git --redact` over full history from a clean `main`
      (a pre-public history scan is the last chance to catch a leaked
      secret cheaply).
- [ ] Review every tracked file: `git ls-files` — no `.local.md`, no
      scratch artifacts, no `artifacts/` staleness we don't want public.
- [ ] Decide the fate of `artifacts/gate-m2-walkthrough.html` (README
      links it) — refresh with a current walkthrough or remove.

## CLA plumbing

- [ ] Create the orphan `signatures` branch for the CLA action:

  ```sh
  git switch --orphan signatures
  git rm -rf . && touch .gitkeep && git add .gitkeep
  git commit -m "chore: initialize CLA signatures"
  git push -u origin signatures && git switch main
  ```

## General repository settings

- [ ] Squash-only merges, auto-delete head branches, no wiki:

  ```sh
  gh repo edit janiorvalle/better-git-review \
    --enable-wiki=false \
    --enable-merge-commit=false \
    --enable-rebase-merge=false \
    --enable-squash-merge=true \
    --delete-branch-on-merge=true
  ```

- [ ] Actions: default workflow permissions `contents: read`.
- [ ] Require approval for workflows from all outside collaborators.
- [ ] Restrict allowed actions to GitHub-authored plus the SHA-pinned
      third-party set already in `.github/workflows/`.
- [ ] Confirm fork PR workflows receive no repository secrets.

## Main branch ruleset

- [ ] Target `main`; require pull requests for everyone including admins.
- [ ] Require one approving review; dismiss stale approvals on new commits.
- [ ] Required status checks: every named CI job (ubuntu / macos /
      windows Go jobs, Verify, Secret scan, Workflow lint, path filter)
      plus `CLA check`.
- [ ] Require branches up to date; require linear history.
- [ ] Block force pushes and deletion; no direct-push bypass for anyone.

## Release tag and environment

- [ ] Tag ruleset for `v*`: creation restricted to the owner.
- [ ] Create the `release` environment; owner as required reviewer
      (pending since Gate M4 — the release workflow already targets it).
- [ ] No long-lived tokens or secrets for releasing: goreleaser publishes
      with the workflow's GITHUB_TOKEN; attestation uses OIDC. Verify the
      release job's permissions stay minimal.

## Security and ownership

- [ ] Enable secret scanning AND push protection (native, free on
      public).
- [ ] Enable private vulnerability reporting.
- [ ] Add/verify CODEOWNERS: `@janiorvalle` for `.github/`, `go.mod`,
      `.goreleaser.yaml`, `install.sh`, and `internal/policy/`.
- [ ] Enable Dependabot for gomod + github-actions.
- [ ] Confirm SECURITY.md, CONTRIBUTING.md, and the CLA check render and
      run as intended; Scorecard workflow goes live automatically once
      public — check its first grade.

## First release

- [ ] Tag `v1.1.0` on main (through the tag ruleset), watch the release
      workflow publish archives + checksums + attestation through the
      protected environment.
- [ ] Smoke-test `install.sh` from the real release on macOS; have a
      Windows user confirm the zip.
- [ ] Only then: announce/share.
