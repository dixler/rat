# Semantic Highlighting Output Spec

This spec describes only the visible output: how source text is colored.

## Goal

Given a source file, produce line-numbered output where identifiers and keywords are colorized consistently.

## Base Rendering

- Every line is shown with a line number.
- Tabs are displayed as 4 spaces.
- Any text not matched by a highlight rule is white.

## Color Map

Use these colors:

- Type: light green
- Variable: yellow
- Parameter: vibrant orange
- Function: light green
- Package/import symbol: purple
- Same-function reference: yellow
- Same-file reference: light green
- Same-package reference: cyan
- Same-project reference: blue
- External reference: purple
- Indirect call target: hot magenta

## Definition vs Reference Appearance

- Definitions are shown as inverted color (reverse video), not plain foreground color.
- Non-definitions (references/usages) are shown as plain foreground color.

Practical shorthand:

- Definition = "color + invert"
- Reference = "color only"

## What Gets Highlighted

Apply highlights for:

- Declarations (types, variables, parameters, functions, etc.).
- References to declarations.
- Import/package reference text.
- Top-level named struct/interface fields.
- Indirect call targets.
- Control-flow keywords:
  - `if`, `else if`, `else`
  - `for`
  - `switch`, `case`, `default`
  - `return`, `break`, `continue`, `fallthrough`

## Block Keyword Coloring Rules

Control-flow block keywords are semantic markers. Color them using these rules:

- `if`, `else if`, `else`
  - Blue by default.
  - Muted orange when that branch has terminal control flow (`return`, `break`, `continue`, `goto`, or `panic`) anywhere in the branch body.

- `for`
  - Blue by default.
  - Muted orange when the loop has escaping control flow (a `break` or `return` in the loop).

- `switch` and `select`
  - Muted orange by default.
  - Green when exhaustive (currently treated as: has a `default` branch).

- `case`
  - Muted orange by default.
  - Blue when it has `fallthrough` and does not have a direct `return`.
  - Muted orange when it has `fallthrough` and also has a direct `return`.

- `default`
  - Always green.

Statement keyword colors used alongside block keywords:

- `return`, `break`: muted orange
- `continue`, `fallthrough`: blue

## Important Overrides

Use these visible overrides when coloring declarations:

- Top-level function definitions appear in light green (definition styling still applies).
- Many top-level declarations and function-local variables appear in yellow (definition styling still applies).

Consistency notes:

- Function and same-file reference use the same color: light green.
- Variable and same-function reference use the same color: yellow.

## Indirect Call Appearance

- Color each character of the indirect call target in hot magenta.
- This is always non-definition styling (plain color, no invert).
- Indirect-call highlighting has higher priority than other overlapping highlights.
- If an indirect-call span overlaps another span, the indirect-call color wins for the overlapping characters.

## Overlap Resolution

If two highlights overlap on the same line:

- Indirect-call spans win over all other span types.
- For non-indirect overlaps, earlier highlight wins.
- A later non-indirect highlight that starts inside already-colored text is ignored.

So manual coloring should proceed left-to-right, keeping the first applicable highlight when collisions happen.

## Manual Coloring Procedure

To reproduce output by hand:

1. Start from white text with line numbers.
2. Mark definitions and references, then apply their colors from the map.
3. Apply declaration overrides (top-level function -> light green, etc.).
4. Apply import/package and control-flow keyword highlights.
5. Apply indirect-call character coloring in hot magenta.
6. Resolve overlaps by keeping the earliest span on each line.
7. Render definitions with invert; render everything else as foreground-only color.
