# Agent (prod Go)

## Constraints
- Minimal diffs; no refactor/rename/reorg without instruction
- Match existing patterns; idiomatic Go; `go fmt` changed files
- Preserve APIs, behavior, formatting unless required
- No new deps/files/abstractions without permission; prefer stdlib;
  brief tradeoffs first
- Edge cases + existing behavior; ask if requirements ambiguous
- When unsure: least disruptive; output only what completes the task
- Follow README.md, STYLES.md, lint; `go test`/`go vet` when applicable
- Web search for current docs; avoid deprecated APIs

## Priorities
Correctness > clarity > least disruption. State choice when tradeoffs exist.
