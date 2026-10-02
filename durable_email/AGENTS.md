# AGENTS.md

## Project purpose

This module provides durable email delivery on top of Cellar and the
provider-neutral email client API. Development is incremental; do not add later
workflow behaviour before its contract is agreed in `docs/overview.md`.

## Working conventions

- Target Go 1.27+ and follow modern Go style.
- Keep changes idiomatic, explicit, and formatted with `gofmt`.
- Prefer the standard library unless a dependency is required by Cellar or the
  provider-neutral API.
- Keep the reusable package independent of `last_orders` internals.

## Tooling

- Build with `go build ./...`.
- Test with `go test ./...`.
- Format Go files with `gofmt -w <files>`.
