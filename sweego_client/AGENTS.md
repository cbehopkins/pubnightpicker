# AGENTS.md

## Project purpose

This repository contains a small Go CLI prototype for experimentally integrating with the Sweego email API.

## Working conventions

- Target Go 1.25+ and follow modern Go style.
- Keep changes idiomatic and well-formatted with `gofmt`.
- Prefer the standard library unless there is a compelling reason to add a dependency.
- Keep code small, explicit, and easy to inspect.
- Avoid abstraction-heavy design in this prototype.

## Tooling and workflow

- Build with:
  - `go build ./...`
- Test with:
  - `go test ./...`
- Format Go code with:
  - `gofmt -w <files>`

## Prototype constraints

- This is an API inspection tool.
- Preserve raw request/response behaviour from Sweego where practical.
- Do not add Pubnight-specific abstractions yet.
