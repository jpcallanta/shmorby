# Agent (prod Go)

## Constraints
- Minimal diffs; no refactor/rename/reorg without instruction
- Match existing patterns; idiomatic Go; `go fmt` changed files
- Preserve APIs, behavior, formatting unless required
- No new deps/files/abstractions without permission; prefer stdlib;
  state tradeoffs first
- Handle edge cases; ask if ambiguous
- When unsure: output only what completes the task
- Follow README.md and lint config; run `go test`/`go vet` when applicable
- Verify against current docs via web search; avoid deprecated APIs

## Priorities
Correctness > clarity > least disruption. State the choice when tradeoffs conflict.

## Style
- Comments: one purpose comment per function, omit symbol name
  ("Returns…"); one per top-level `if` only, none for nested
- Formatting: max 80 columns in any edited/generated text file; blank line
  around assignment/var groups, top-level `if` blocks, every `return`
- Functions: small, testable units; break only when needed for correctness
- Errors: return on fatal; wrap `fmt.Errorf("ctx: %w", err)`
- Tests: `TestFuncName_Scenario[_ExpectedOutcome]`; errors "want X, got Y"
- Structure: Cobra `RunE` for failing commands; flags in package `var ()`;
  register in `init()`
- Imports: stdlib, blank line, third-party (`goimports`)
