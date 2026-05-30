# Text Semantic Highlight (VS Code)

This extension fetches semantic spans from a `rat` HTTP server and decorates those ranges.

Only spans returned by `rat` are decorated. Text outside those ranges is left to VS Code's normal syntax highlighting so editor syntax colors do not cover rat span colors.

## Auto-start server

By default, the extension starts the server process automatically:

- command: `rat`
- args: `--serve --addr :8081`
- cwd: `${workspaceFolder}`

For extension development inside this repository, set the command to `go` and the args to `run ./cmd/rat --serve --addr :8081`.

Config keys:

- `textSemanticHighlight.autoStartServer`
- `textSemanticHighlight.serverCommand`
- `textSemanticHighlight.serverArgs`
- `textSemanticHighlight.serverCwd`
- `textSemanticHighlight.serverUrl`
- `textSemanticHighlight.languages`

## Run extension

1. Open the repo/workspace in VS Code.
2. Press `F5`.
3. Open/save a Go file to refresh decorations.
