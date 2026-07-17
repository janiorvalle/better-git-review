# Contributing

## Development Setup

Requirements:

- Go 1.24 or newer
- Git
- Optional: `claude`, `codex`, or an OpenRouter key for opt-in provider tests
- Optional: GoReleaser v2 for release configuration validation

Clone the repository and run the deterministic suite:

```sh
make verify
```

`make verify` is the same local gate CI runs: build, vet, tests, GoReleaser
configuration validation, snapshot archives, and an installed-artifact smoke
test. CI also runs `shellcheck` and exercises `install.sh` against a local
GoReleaser snapshot, never a real release.

Tests must not depend on an LLM, network access, a user configuration file, or
a populated cache. The mock provider is the default tool for end-to-end tests.

## Test Policy

Add focused unit tests for parsers, validation, prompt construction, config
merging, cache behavior, and other non-trivial pure logic.

End-to-end tests live in `test/e2e`. They build the real binary and invoke it
as a subprocess against fixture diffs. Cover exit codes, stderr decisions,
generated JSON, and rendered HTML when behavior crosses package boundaries.

The real Claude test is opt-in:

```sh
BGR_E2E_CLAUDE=1 go test ./test/e2e/ -run Claude -v -count=1
```

It must continue to skip cleanly when the environment variable or executable
is absent. Real-provider tests never run in normal CI.

Before opening a pull request, run:

```sh
make verify
```

Install the optional pre-commit secret scan after installing `gitleaks`:

```sh
make install-hooks
```

## Release Setup

The repository owner must create a GitHub environment named `release` under
Settings > Environments and add the desired deployment protection rules. The
tag workflow is already bound to that environment; repository settings are not
changed by the workflow itself.

## Writing A Provider

Providers are deliberately small. The core owns prompt construction, untrusted
data framing, JSON extraction and repair, schema validation, retries,
seatbelts, staged analysis, caching, and rendering.

Implement the three-method interface in `internal/provider`:

```go
type Provider interface {
    Name() string
    Detect() (available bool, detail string)
    Complete(ctx context.Context, prompt string) (string, error)
}
```

`Name` returns the stable configuration name. `Detect` checks whether the
provider is usable without making a billable generation call. `Complete`
accepts the complete prompt and returns model text without attempting to parse
or normalize it.

Providers that can enforce JSON Schema natively should also implement:

```go
type StructuredProvider interface {
    Provider
    CompleteStructured(
        ctx context.Context,
        prompt string,
        schema json.RawMessage,
    ) (json.RawMessage, error)
}
```

Structured providers still receive semantic validation and deterministic
seatbelts from the core. They skip only free-form extraction and repair.

Providers may implement `Cataloger` to supply model choices and supported
reasoning levels to `bgr configure`. Catalog failures must have a deterministic
offline fallback; normal tests never call a live catalog. Unknown model IDs
remain valid because provider catalogs go stale.

### Detection Etiquette

- Detection must be fast and side-effect free.
- Do not make network or generation calls from `Detect`.
- Report a concise useful detail, such as an executable path or missing
  environment variable.
- Never log or return secret values.
- Explicit provider selection should return a clear runtime error when its
  credential or executable is unavailable.
- Do not silently fall back to another provider after selection.

### Integration Checklist

1. Add the provider implementation and focused tests.
2. Register its explicit name and model defaults in `provider.Select`.
3. Add it to automatic detection only when detection is reliable and free.
4. Add user and repository config fields only when the provider needs them.
5. Keep repository-provided endpoints or credential-variable names inside the
   existing trust flow.
6. Add deterministic subprocess or HTTP tests. Do not require live credentials.
7. Update the README provider setup section.

A typical CLI adapter should remain around 50 lines because the core guarantees
the same validation, retry, staging, security, and output behavior for every
provider.

## Writing A Source Adapter

The core does not depend on GitHub. Source adapters resolve a user selection
into a unified diff plus source metadata; parsing, analysis, caching, and
rendering stay shared. Implement `source.Source`, keep `Detect` fast and
side-effect free, and register the adapter in `internal/app/registries.go`.

Adapters should return actionable detection errors, use `internal/gitexec` for
local git operations, avoid changing the working tree, and add unit plus
subprocess e2e coverage. GitLab and other forges are intentionally community
contribution territory.

## Agent Integrations

The schema-versioned `bgr --format json` output is the machine contract. The
binary embeds a matching agent skill that users may install explicitly through
`bgr configure`. An MCP wrapper is deferred until a shell-less host needs one;
a thin community adapter over the JSON contract is welcome.

## Licensing

Contributions are licensed under the MIT License and the project's contributor
license agreement. The CLA check runs automatically on a contributor's first
pull request.
