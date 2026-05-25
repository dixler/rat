# Text Semantic Highlight (VS Code)

This extension fetches semantic spans from a `rat` HTTP server and decorates those ranges.

## Auto-start server

By default, the extension starts the server process automatically:

- command: `go`
- args: `run ./cmd/rat --serve --addr :8081`
- cwd: `${workspaceFolder}`

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
