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

## Comments
- One concise purpose comment per function; omit symbol name
  (e.g. "Returns…", not "foo returns…")
- One comment per top-level `if` (what it checks); none for nested `if`

## Formatting
- Max 80 columns per line; break into multiline style when needed, this appiles
  to any text based file generated or edited (markdown, source files, yaml, etc)
- Blank line before/after: assignment groups, var decl groups,
  top-level `if` blocks, every `return`

## Functions
- Small, testable units; break only when needed for correctness

## Errors
- Return on fatal error; wrap: `fmt.Errorf("ctx: %w", err)`

## Tests
- `TestFuncName_Scenario_ExpectedOutcome` or `TestFuncName_Scenario`
- `t.Errorf`/`t.Fatalf`: "want X, got Y"

## Structure
- Cobra `RunE` for failing commands; flags in package `var ()`;
  register in `init()`

## Imports
- stdlib, blank line, third-party (`goimports`)
