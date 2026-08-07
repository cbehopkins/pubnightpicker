# AGENTS.md

## Project purpose

This repository is a Go library for a lightweight execution runtime named Cellar, with CLI tools for debugging and local inspection. Prefer small, explicit modules over abstraction-heavy designs.

## Working conventions

- Target Go 1.24+ and follow modern Go style.
- Keep changes idiomatic and well-formatted with `gofmt`.
- Prefer the standard library unless there is a compelling reason to add a dependency. If a dependency is justified, document the rationale clearly.
- Keep the public API simple and avoid over-designing. This project is still evolving, so API changes are acceptable when they improve clarity.
- Use small, focused packages and avoid introducing cross-cutting complexity unless it is genuinely needed.

## Architecture guidance

- Keep business logic and infrastructure concerns separated.
- Preserve the runtime boundaries described in [README.md](README.md) and the ADRs in [docs/adr](docs/adr).
- For storage, concurrency, retries, and execution semantics, consult the relevant ADRs and CDDs before making changes.
- If behaviour changes, update the relevant ADR or design note rather than leaving the decision implicit.

## Testing expectations

- Follow a red/green workflow: add or update a test first, make it pass, and only then refactor.
- Prefer tests that exercise real behaviour and keep them easy to understand.
- Use whatever test style fits the case best: table-driven tests, fixtures, or focused unit tests.

## Tooling and workflow

- Run tests with:
  - `go test ./...`
- Format Go code with:
  - `gofmt -w <files>`
- If available, run linting with:
  - `golangci-lint run`

## Repository-specific notes

- This repo is a subfolder of a larger repository. Keep changes local to this module and do not assume parent-repo CI or workflows should be changed unless explicitly requested.
- CLI/debug tooling should stay small and focused; keep it separate from core library behaviour unless the change is clearly part of the runtime contract.
- When adding new behaviour, prefer explicit documentation in the ADRs over hidden implementation choices.
