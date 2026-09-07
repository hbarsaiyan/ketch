# Contributing to ketch

Thanks for helping. Bug fixes, new functionality, documentation, and new
backend providers are all welcome. This page says what a pull request needs
in order to be merged, and one rule that is not negotiable.

Before proposing a feature, read the
[Non-Goals & Scope](design/DESIGN.md#non-goals--scope) section of the design
doc. If a change lands on the wrong side of one of those lines it belongs in a
different tool, and a PR for it will be closed with a pointer to that section.

## Setup

```bash
git clone https://github.com/1broseidon/ketch && cd ketch
git config core.hooksPath .githooks   # gofmt, vet, lint, tests on every commit
make build                            # ./ketch
make lint                             # golangci-lint (gocyclo max 15)
make test                             # go test ./...
```

Go 1.25 or newer, `CGO_ENABLED=0`, pure Go. `golangci-lint` must be on PATH for
the hook and `make lint`.

## The one hard rule: backend providers go through the registry

**If your PR adds a search, code, or docs backend, it must use the registry
pattern. PRs that wire a provider any other way will not be merged, and will
be sent back for a rewrite before review.**

A provider is one Go file, its tests, one registry line, and a regenerated
golden fixture. No production consumer changes, ever:

1. `search/<name>.go` (or `code/`, `docs/`) containing the implementation, its
   descriptor function, and its health probe.
2. One `<name>Provider()` call appended to the ordered `providers` slice in
   that package's `registry.go`.
3. `search/<name>_test.go` covering request building, result mapping,
   authentication, cancellation, and the error and retry paths that matter
   for that API.
4. `UPDATE_REGISTRY_GOLDENS=1 go test ./cmd/ ./doctor/` to add the provider's
   own discovery keys and doctor row to the golden fixtures. That is the only
   shared file a provider PR touches, and the diff must be purely additive.

Everything else follows from the descriptor: config keys, `KETCH_*` env
overrides, redacted `ketch config` discovery, `ketch doctor`, CLI backend
lists and help, MCP tool and argument descriptions, and `--multi` / `--random`
eligibility. The descriptor fields and the rules for each are documented in
[AGENTS.md § Adding a provider](AGENTS.md#adding-a-provider); the cross-package
test in `search/registry_test.go` shows one registration flowing through every
consumer.

A backend PR will be rejected if it does any of the following:

- Adds a `case "<name>"` or name check to `cmd/`, `mcp/`, `doctor/`, or
  `config/`. Consumers do not know provider names.
- Adds fields to the config struct or to the discovery payload by hand.
  Declare `Settings` on the descriptor instead.
- Registers through `init()` or any form of auto-discovery. The slice is
  explicit and ordered on purpose.
- Adds a dependency for a plain HTTP + JSON API. `net/http` and
  `encoding/json` are the standard here.
- Rewrites existing lines in the config or doctor golden fixtures. A new
  provider only adds its own keys and doctor row; if any other line moves, the
  wiring is wrong.
- Probes or validates credentials inside the factory. `New` only constructs a
  client and must accept empty credentials; `Usable` decides eligibility
  without network I/O; `Probe` is the only place that talks to the service.

Merging a provider means it is supported through the registry. It does not
make it a recommended default, and it does not by itself earn a row in the
README backend tables or the docs site; provider recommendation is a separate
product decision and the documentation update is a separate change. Please say
in the PR whether you are affiliated with the service. That is not a problem,
it just belongs in the description.

## Everything else

Fixes, features inside scope, extraction improvements, docs, and skill or MCP
refinements are welcome without any special ceremony. What a mergeable PR looks
like:

- **Small and single-purpose.** One fix or one feature per PR. Refactors travel
  separately from behavior changes.
- **Tests for the change.** Table-driven where it fits, `httptest` for anything
  that talks HTTP, no live network in `go test`.
- **Green locally.** `make lint` and `make test` pass; the pre-commit hook
  enforces this if you installed it.
- **Output contracts intact.** Frontmatter keys, `--json` shapes, `--minimal`
  columns, exit codes, and the `[validation]` / `[precondition]` /
  `[upstream]` / `[not_found]` / `[cancelled]` error prefixes are read by
  agents. Changing them is a breaking change and needs a changelog entry and a
  reason.
- **No new dependencies without a case.** A pure-Go library that removes a real
  burden is fine. A framework to save twenty lines is not. See the design doc.
- **A changelog line** under `## [Unreleased]` in `CHANGELOG.md` for anything a
  user or contributor would notice.
- **Docs updated in the same PR** when behavior visible to users changes:
  `README.md`, `CLAUDE.md`, `AGENTS.md`, and the relevant page under `site/`.

Commit messages follow the existing history: a short prefix such as
`feat(mcp):`, `fix(scrape):`, `docs:`, or `refactor:`, an imperative summary,
and the issue number when there is one.

## Reporting problems

Open an issue at [github.com/1broseidon/ketch/issues](https://github.com/1broseidon/ketch/issues)
with the command you ran, the output, `ketch version`, and `ketch doctor`
output if a backend is involved. `ketch config` prints a redacted view of your
settings that is safe to paste; it never prints credential values.
