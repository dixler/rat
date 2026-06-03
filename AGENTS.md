# AGENTS.md

Guidance for AI coding agents working in this repository.

Always review and update the AGENTS.md to ensure it's up to date with the high level product details and high level architectural overview.

## Project Overview

`rat` is an experimental semantic highlighter for Go. It provides:

- A CLI and local HTTP server in `cmd/rat`.
- Shared Go highlighting/file/display logic under `internal/`.
- A Lambda-oriented entry point in `cmd/highlight-lambda` and a local helper in `cmd/highlight-local`.
- A VS Code extension in `vscode-text-semantic` that consumes spans from the local `rat` server.
- Deployment/site support under `infra/`.

The core behavior is semantic highlighting, not plain syntax highlighting. Prefer preserving the existing model of spans, declaration/reference coloring, control-flow coloring, and shared output behavior across terminal, HTTP, and VS Code consumers.

## Key Areas

- `cmd/rat/`: CLI, local server, and pipeline golden tests.
- `internal/`: shared highlighting, file loading, rendering, `gopls`, and API logic.
- `testdata/`: golden outputs used by tests.
- `vscode-text-semantic/`: VS Code extension and extension tests.
- `infra/`: Pulumi deployment code and static site assets.

## Toolchain

- Go version is `1.26` per `go.mod` and `go.work`.
- The Go workspace is rooted at this repository via `go.work`.
- `github.com/stretchr/testify` is replaced with `./third_party/testify`; do not remove this unless the dependency strategy changes intentionally.
- Node/npm are used for the VS Code extension and Pulumi infra.

## Commands

Run Go tests:

```bash
go test ./...
```

Update Go golden outputs after intentional highlighting changes:

```bash
ACCEPT=1 go test ./...
```

Build the CLI:

```bash
go build ./cmd/rat
```

Build the embedded `gopls` artifact:

```bash
go build -o internal/goplsbin/gopls golang.org/x/tools/gopls
```

Build the main project targets:

```bash
make
```

Run the local server:

```bash
go run ./cmd/rat --serve --addr :8081
```

Run extension tests:

```bash
cd vscode-text-semantic
npm test
```

Build the extension package:

```bash
cd vscode-text-semantic
npm run build
```

Build Pulumi TypeScript:

```bash
cd infra/pulumi
npm run build
```

## Development Guidelines

- Keep changes small and behavior-focused. This project is experimental, but avoid adding complexity unless it improves the highlighting signal or developer ergonomics.
- Preserve compatibility between CLI rendering, HTTP span payloads, and VS Code decoration behavior when touching span generation or formats.
- Prefer extending existing pipeline/service/rendering code over introducing parallel implementations.
- Update golden testdata with `ACCEPT=1 go test ./...` when intentional highlighting output changes affect expected output.
- Run `gofmt` on changed Go files.
- Do not commit generated binaries or extension packages unless the user explicitly asks. Existing generated artifacts may already be present; avoid touching them unless necessary.
- Avoid changing `third_party/` unless the task is specifically about vendored/replaced dependencies.
- Rebuild `internal/goplsbin/gopls` only when necessary.
- If a relevant test/build command is skipped because dependencies, time, or environment are unavailable, state that clearly in the final response.

## API Notes

The local server exposes `POST /spans` and expects a JSON body like:

```json
{ "path": "/absolute/path/to/file.go" }
```

It returns spans grouped by 1-based line number. Keep this shape in mind when editing `cmd/rat`, `internal/highlightapi`, or the VS Code extension.

## Infra Safety

- Do not run `pulumi up`, `npm run preview`, install VS Code extensions, or deploy infrastructure unless explicitly requested.
- Treat `infra/pulumi` as deployment code and `infra/site` as static assets; validate locally before proposing deployment.
