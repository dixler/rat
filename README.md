# rat

You've seen `cat`, you've seen `bat`, but have you seen `rat`?

`rat` is an experimental semantic highlighter for Go. It does not just color tokens by syntax. It tries to color names by where their declarations live, color control-flow keywords by what they imply, and share the same semantic spans with the terminal and VS Code extension.

Run it on a file:

```bash
rat main.go
```

![cli-example](./.images/cli.png)

You can also run it as a VS Code plugin.

Dislaimer: I vibe coded most of this (including the README). Dev-tools can afford to be probably right as long as they help more than they hurt and the developer is aware. I'm focused more on the ergonomics of this tool rather than the correctness at the moment. If you find any errors, please let me know.

## Motivation


Syntax highlighting is useful, but it mostly repeats information you can already see. A keyword is visibly a keyword. An identifier is visibly an identifier.

The harder questions are usually semantic:

- Is this name local to the function, the file, or coming from somewhere else?
- Is this package from this repository or an external dependency?
- Is this branch a guard clause or does execution continue after it?
- Is this call statically obvious or is it going through an interface/function value?

`rat` tries to put that information directly on the page.

## Reference Coloring

Reference colors are based on the relationship between the place where a name is used and the place where that name is declared.

- Vibrant orange: same function/local reference.
- Magenta: parameter.
- Green: same file, outside the current function.
- Light blue: same package, different file.
- Blue: same project/repository, different package.
- Purple: external dependency or unknown target.
- White: Go built-in. (or I just didn't color it yet).

Declarations use an inverted/background style. That makes definitions stand out from references.

Struct field names are colored by the relationship between the struct type declaration and the field type declaration. For struct literals whose type is declared outside the current package, package-level resolution is used: brown for built-ins, green for the struct type's package, blue for the same project, and purple for external dependencies or unknown targets.

Examples:

- A local variable used later in the same function is vibrant orange.
- A package-level identifier in the same file is green.
- A symbol imported from another package in your module is blue.
- A symbol from a different repository is purple.

## Block Coloring

Control-flow colors are meant as a shorthand for how a block behaves.

### `if`, `else if`, and `else`

- Brown: the branch contains terminal control flow like `return`, `break`, `continue`, `goto`, or `panic`.
- Blue: the branch can exit normally and continue after the block.

This makes guard clauses visually distinct from ordinary branches.

### `for` and `range`

- Brown: the loop may escape through `break` or `return`.
- Blue: no escaping `break` or `return` was found by `rat`.

`continue` is highlighted separately, but it does not make the loop itself brown because it stays inside the loop.

### `switch` and type `switch`

- Green: the switch has a `default` case, so `rat` treats it as exhaustive.
- Brown: the switch has no `default` case.

Cases also get colored:

- Green: `default` case.
- Blue: case with `fallthrough`.
- Brown: ordinary case.

### `select`

`select` uses the same model as `switch`:

- Green: has a `default` case.
- Brown: has no `default` case.

Its communication clauses are displayed as cases:

- Green: `default` clause.
- Brown: ordinary communication clause.

### Other Control Flow

- `return`: brown when returning a non-`nil` error, blue otherwise.
- `break`: brown.
- `continue`: blue.
- `fallthrough`: blue.
- Matching braces for recognized blocks get the same color as the block keyword.

## Indirect Calls

Indirect calls are magenta.

That includes calls through interfaces or function values. The color is intentionally loud because the concrete target is less obvious from the call site than a direct function call.

## Other Highlighting

- Comments are gray.
- Top-level named struct fields are highlighted like declarations.
- Imports are colored by whether the imported package resolves inside the project or outside it.
- Unhighlighted text stays white in terminal output. In VS Code, only spans returned by `rat` are decorated so editor syntax colors do not cover rat span colors.

## Requirements

- Go 1.26 or newer, matching this repo's `go.mod`.
- Node.js and npm if you want to build the VS Code extension package.
- VS Code if you want editor decorations.

## Install The CLI And VS Code Extension

Make sure `$HOME/bin` exists and is on your `PATH`:

```bash
mkdir -p "$HOME/bin"
export PATH="$HOME/bin:$PATH"
```

Install the VS Code extension dependencies once:

```bash
cd vscode-text-semantic
npm install
cd ..
```

Build everything:

```bash
make
```

`make` does four things:

1. Builds `internal/goplsbin/gopls` so it can be embedded.
2. Builds `rat` in the repository root.
3. Builds the VS Code `.vsix` package and moves it to the repository root.
4. Runs `rat ./cmd/rat/main.go` and screenshots the colored terminal output to `.images/cli.png`.

Install the CLI and generated extension package:

```bash
make install
```

Install the generated extension package:

```bash
code --install-extension text-semantic-highlight-*.vsix
```

You can also install it from VS Code by right-clicking the generated `.vsix` file and choosing `Install Extension VSIX`.

## CLI Usage

Print ANSI-colored output:

```bash
rat path/to/file.go
```

Generate HTML:

```bash
rat -format html path/to/file.go
```

Run the local HTTP server used by the VS Code extension:

```bash
rat --serve --addr :8081
```

The server accepts `POST /spans`:

```json
{ "path": "/absolute/path/to/file.go" }
```

It returns flattened JSON spans grouped by 1-based line number:

```json
{
  "spans": {
    "7": [
      { "start": 5, "end": 10, "style": "\u001b[38;5;226m" }
    ]
  }
}
```

## VS Code Usage

After installing the extension, open a Go workspace and open or save a Go file.

By default, the extension starts:

```bash
rat --serve --addr :8081
```

Then it calls `http://localhost:8081/spans` for visible Go files and turns the returned ANSI styles into VS Code decorations.

The extension keeps decoration state per document, refreshes visible editors on active-editor changes, saves, and relevant configuration changes, and normalizes both grouped and legacy flat span payloads for tests and local development.

Useful settings:

- `textSemanticHighlight.enabled`: turn highlighting on or off.
- `textSemanticHighlight.serverUrl`: server URL, default `http://localhost:8081`.
- `textSemanticHighlight.languages`: language IDs to decorate, default `go`.
- `textSemanticHighlight.autoStartServer`: whether the extension starts the server.
- `textSemanticHighlight.serverCommand`: command to start the server, default `rat`.
- `textSemanticHighlight.serverArgs`: server arguments, default `--serve --addr :8081`.
- `textSemanticHighlight.serverCwd`: working directory for the server.

Command palette command:

```text
Text Semantic Highlight: Toggle
```

For extension development from this repository, you can set:

```json
{
  "textSemanticHighlight.serverCommand": "go",
  "textSemanticHighlight.serverArgs": ["run", "./cmd/rat", "--serve", "--addr", ":8081"],
  "textSemanticHighlight.serverCwd": "${workspaceFolder}"
}
```

## Troubleshooting

If VS Code shows no colors:

- Confirm `rat` is on your `PATH`: `rat --serve --addr :8081` should start a server.
- Check the VS Code output channel named `Text Semantic Highlight`.
- Save the file. The extension refreshes on active editor changes and saves.
- Make sure the file is in a Go module/workspace that Go tooling can load.
- Make sure port `8081` is not already used by another process.

If `rat` cannot find or run `gopls`:

- Rebuild with `make` so `gopls` is embedded before `rat` is compiled.
- Or set `GOPLS_BIN=/path/to/gopls` to force a specific `gopls` binary.

If a color seems wrong:

- `rat` is conservative and experimental; some cross-package or dynamic cases depend on what `gopls` can resolve.
- Interface calls and function-value calls are intentionally marked as indirect.
- Declaration backgrounds are intentional so definitions are easy to spot.

## Development Commands

Run tests:

```bash
go test ./...
```

Run the VS Code extension parity tests:

```bash
cd vscode-text-semantic
npm test
```

Build the embedded `gopls` artifact:

```bash
go build -o internal/goplsbin/gopls golang.org/x/tools/gopls
```

Build just the CLI in the repo root:

```bash
go build ./cmd/rat
```

Build just the VS Code extension:

```bash
cd vscode-text-semantic
npm run build
```

Regenerate the CLI screenshot:

```bash
make .images/cli.png
```

## Known Limitations And Next Steps

This is experimental and Go-specific today.

Future directions could include using a more generic parser such as tree-sitter, sharing more graph logic across languages, improving dynamic call detection, and making sticky-scroll/editor integrations behave better with decorations.

Contributions are welcome. Please keep changes understandable and avoid adding complexity that is not buying better signal in the editor.
