# Development Guidelines

This repo follows the same host-side Go formatting and commenting rules as
`bip32-pq-zkp/docs/code-format.md`.

The intent is simple: the two repos should read like they were written by the
same team, because they are part of the same stack.

## What We Optimize For

- complete godoc comments, especially on exported APIs
- code broken into readable stanzas rather than one uninterrupted block
- inline comments that explain intent, not the obvious
- best-effort 80-column wrapping for long calls and long strings
- command entrypoints that stay thin while reusable logic lives in normal Go
  packages

## Practical Rules

- Every exported function, type, method, constant, variable, and exported
  struct field should have a proper doc comment.
- Unexported helpers should also be commented when their purpose is not
  instantly obvious.
- Long function calls should use one argument per line once they wrap.
- Logical stages inside a function should be separated by blank lines and, when
  useful, short comments.
- Host-side code should be easy to scan with `go doc` and not force readers to
  reverse-engineer business logic from a `cmd/` entrypoint.

## Tooling

The repo Makefile should remain the canonical entrypoint for:

- `make fmt`
- `make fmt-check`
- `make tidy`
- `make tidy-check`
- `make lint-native`
- `make lint`

Those checks are intentionally local-first and focused on the native Go
packages that can be validated without the TinyGo guest target.
