# Contributing to Terrablade

Thank you for helping improve Terrablade. Bug reports, focused feature
proposals, documentation fixes, and pull requests are welcome.

## Before you start

Search existing issues before opening a new one. For a substantial change,
especially one that changes formatted output or public API, open an issue first
so the intended behavior can be agreed on before implementation.

Formatting changes affect every user and must preserve deterministic,
idempotent output. Include a minimal input, the current output, the proposed
output, and the reason the new form is preferable.

Do not report suspected security vulnerabilities in a public issue. Follow the
[security policy](.github/SECURITY.md) instead.

## Development setup

Install the Go toolchain declared in `go.mod`, clone the repository, and run:

```console
go test ./...
```

Terraform and OpenTofu are not build dependencies. They are only needed to run
the optional compatibility tests:

```console
TERRABLADE_REFERENCE_CLI=terraform go test ./...
TERRABLADE_REFERENCE_CLI=tofu go test ./...
```

CI currently tests against Terraform 1.16.3 and OpenTofu 1.12.6.

## Repository structure

- `internal/syntax` lexes and parses native HCL into a lossless concrete syntax
  tree.
- `internal/lowering` converts syntax into canonical formatting decisions and
  applies expression normalization.
- `internal/document` contains the document model and renderer.
- The root `terrablade` package exposes the public Go API and diagnostics.
- `cmd/terrablade` implements the command-line interface.
- `tools/genunicode` regenerates the pinned Unicode identifier tables.

Keep these boundaries intact unless a change has a clear architectural reason
to move them.

## Making changes

Keep pull requests focused and add tests for observable behavior. Prefer small,
synthetic HCL inputs over copied real-world configurations. A formatter test
should make the policy it protects apparent from its name and expected output.

Run `gofmt` on changed Go files. Before submitting a pull request, run at least:

```console
go test ./...
go test -race ./...
go vet ./...
```

CI also runs `staticcheck`, `shadow`, `predeclared`, compatibility checks, and
tests on Linux, macOS, and Windows. The workflow files contain the exact pinned
tool versions.

Pull request titles and commits intended for `master` should follow
[Conventional Commits](https://www.conventionalcommits.org/). Release Please
uses that vocabulary to determine versions and generate release notes. Common
types in this repository are `feat`, `fix`, `refactor`, `test`, `docs`, and
`ci`. Mark breaking changes explicitly with `!` or a `BREAKING CHANGE` footer.

## Generated Unicode tables

Do not edit `internal/syntax/unicode_tables.go` by hand. If the pinned Unicode
version changes, update the source URL and checksum in `tools/genunicode`, then
regenerate from the repository root:

```console
go generate ./internal/syntax
go test ./internal/syntax ./tools/genunicode
```

Review the generated diff. A Unicode update can change token boundaries and
therefore requires explicit compatibility consideration.

## Fuzzing

Fuzz targets exist across the parser, lowering, renderer, public formatter, and
CLI. Run a focused target from its package, for example:

```console
go test -fuzz=FuzzFormat -fuzztime=30s .
```

Only check in a discovered fuzz input when it reproduces a fixed regression or
adds coverage not provided by existing seeds. The syntax corpus policy and
measurement procedure are documented in
`internal/syntax/testdata/fuzz/README.md`.

## Pull requests

A pull request should explain what changed, why the change belongs in
Terrablade, and how it was verified. Maintainers may ask for smaller commits,
additional edge cases, or a policy discussion when a change affects canonical
output.

By contributing, you agree that your contributions are licensed under the
project's Apache License 2.0.
