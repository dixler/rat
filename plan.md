1. **Deduplicate `collectSpans` and `ParseFormats` sorting logic:**
   - I'll create a single `sortSpans` function in `cmd/rat/format.go` to handle the span sorting logic that is currently duplicated.
   - I will replace the duplicate sorting loops in `collectSpans` and `ParseFormats` with calls to `sortSpans`.

2. **Add `Yellow` style:**
   - In `internal/display/render.go`, add `Yellow = "\x1b[33m"` so that I have the complete rainbow (R: Red, O: Orange, Y: Yellow, G: Green, B: Blue, V: Purple).

3. **Update parser to identify indirect calls:**
   - Modify `internal/file/scan/build.go`'s `Result` struct to add `IndirectCalls []IndirectCall`.
   - The AST `Inspect` loop will now identify indirect `ast.CallExpr` nodes. If a call is determined to be indirect, its location and text length (or just text) will be recorded. For example, if it's an `ast.Ident` or `ast.SelectorExpr`, I'll track `File`, `Line`, `Column`, and `Name` (to get the length). If it's another expr, track the whole `Fun` node location. Wait, we should specifically highlight the identifier according to the prompt: "Each letter of the identifier should be highlighted...". If it's `i.M()`, the identifier is `M`. If it's `fn()`, the identifier is `fn`. If it's `fns[0]()`, it's the whole `fns[0]`? The prompt says "Whenever a function call cannot be statically determined... Each letter of the identifier should be highlighted...". I'll use the `Text` or `Length` of whatever we consider the "identifier". Wait, for `ast.SelectorExpr`, the identifier is `Sel.Name`. For `ast.Ident`, it's `id.Name`. For others, I can just use `File, Line, Column` and a default string representation or `Length`. Let's create an `IndirectCall` struct with `File`, `Line`, `Column`, and `Text` fields.

4. **Propagate `IndirectCalls` to the model:**
   - Update `internal/file/build.go` to parse the `raw.IndirectCalls` into `[]file.Location` or a new interface `file.IndirectCall` that provides the text length. I can just return `[]file.IndirectCall` with `Location()` and `Text()`.
   - Update `internal/file/file.go`'s `File` interface to include `IndirectCalls() []IndirectCall`. Add the implementation to the `file` struct.

5. **Format indirect calls in `ParseFormats`:**
   - Modify `ParseFormats` in `cmd/rat/format.go` to iterate over `f.IndirectCalls()`.
   - For each indirect call, generate individual `display.Span` items for each character of its `Text()`.
   - Use the colors R, O, Y, G, B, V mapped to R=Red, O=Orange, Y=Yellow, G=Green, B=Blue, V=Purple. Keep repeating them using modulo.
   - Insert these spans into the map by line number.

6. **Pre-commit checks:**
   - Call the `pre_commit_instructions` tool to get the pre-commit checks and run them.
