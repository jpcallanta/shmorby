# Style

## Comments
- One concise purpose comment per function; omit symbol name
  (e.g. "Returns…", not "foo returns…")
- One comment per top-level `if` (what it checks); none for nested `if`

## Formatting
- Max 80 columns per line; break into multiline style when needed
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
