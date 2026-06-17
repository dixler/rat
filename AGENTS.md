# AGENTS.md

Guidance for AI coding agents in this repository.

## User-Driven Code Style

Build and maintain an impression of the user's desired code style from prompts and feedback. This is the foremost development guideline: all other development impressions should stem from observed user preferences and durable guidance recorded here.

Learn continuously. Keep the working impression current during each task, and update Code Style Learnings when durable preferences emerge.

## Code Style Learnings

Agent-maintained. Update when prompts or feedback reveal stable, reusable code preferences.

- Prefer clearly separated, purpose-specific sections for durable guidance instead of mixing requirements with reference documentation.
- Prefer context-efficient communication and edits; compact AGENTS.md when appropriate without losing useful guidance, and avoid unnecessary detail, churn, or broad rewrites.
- Prefer removing words and lines over adding them when both options are correct.
- Prefer not creating new files for code that is only referenced in one place.
- Preserve package boundaries; do not make rendering/highlight code depend directly on language-specific scanner packages.
- Rename identifiers (variables, functions, types, fields, etc.) if a more appropriate or accurate name would fit.

## Documentation Requirements

- Review and update Documentation when repository behavior changes.
- Product changes require updated feature documentation.
- Architecture changes require updated responsibilities, data flows, commands, or constraints.

## Documentation

Agent-maintained. Update when product behavior, architecture, data flows, commands, or constraints change.

### Project Overview

`rat` is an experimental semantic highlighter for Go with:

- A CLI and local HTTP server in `cmd/rat`.
- Shared highlighting, file loading, rendering, `gopls`, and API logic under `internal/`.
- A Lambda-oriented entry point in `cmd/highlight-lambda` and a local helper in `cmd/highlight-local`.
- A VS Code extension in `vscode-text-semantic` that consumes spans from the local `rat` server.
- Deployment/site support under `infra/`.

Core behavior is semantic highlighting, not plain syntax highlighting. Preserve spans, declaration/reference coloring, control-flow coloring, and shared output behavior across terminal, HTTP, and VS Code consumers. Go semantic parsing uses Go AST/type information and `gopls`.

### Key Areas

- `cmd/rat/`: CLI, local server, and pipeline golden tests.
- `internal/`: shared highlighting, file loading, rendering, generic LSP, `gopls`, and API logic.
- `testdata/`: golden outputs used by tests.
- `vscode-text-semantic/`: VS Code extension and extension tests.
- `infra/`: Pulumi deployment code and static site assets.

### API Notes

The local server exposes `POST /spans` with:

```json
{ "path": "/absolute/path/to/file.go", "content": "optional in-memory source" }
```

It returns spans grouped by 1-based line number. Preserve this shape when editing `cmd/rat`, `internal/highlightapi`, or the VS Code extension.

## Toolchain

- Go `1.26` per `go.mod` and `go.work`; workspace rooted via `go.work`.
- `github.com/stretchr/testify` is replaced with `./third_party/testify`; keep unless dependency strategy changes intentionally.
- `scan.Result.Nodes` carries semantic coloring nodes, including lexical syntax/literal/builtin nodes and generated function/control-flow nodes; avoid reintroducing file-level token adapters.
- Node/npm are used for the VS Code extension and Pulumi infra.

## Commands

- Go tests: `go test ./...`
- Update Go goldens after intentional highlighting changes: `ACCEPT=1 go test ./...`
- Build CLI: `go build ./cmd/rat`
- Build embedded `gopls`: `go build -o internal/file/scan/golang/goplsclient/gopls golang.org/x/tools/gopls`
- Build main targets: `make`
- Run local server: `go run ./cmd/rat --serve --addr :8081`
- Extension tests: from `vscode-text-semantic`, run `npm test`
- Extension build: from `vscode-text-semantic`, run `npm run build`
- Pulumi build: from `infra/pulumi`, run `npm run build`

## Development Guidelines

- Keep changes small and behavior-focused. This project is experimental, but avoid adding complexity unless it improves the highlighting signal or developer ergonomics.
- Preserve compatibility between CLI rendering, HTTP span payloads, and VS Code decoration behavior when touching span generation or formats.
- Prefer extending existing pipeline/service/rendering code over introducing parallel implementations.
- Update golden testdata with `ACCEPT=1 go test ./...` when intentional highlighting output changes affect expected output.
- Run `gofmt` on changed Go files.
- Do not commit generated binaries or extension packages unless the user explicitly asks. Existing generated artifacts may already be present; avoid touching them unless necessary.
- Avoid changing `third_party/` unless the task is specifically about vendored/replaced dependencies.
- Rebuild `internal/file/scan/golang/goplsclient/gopls` only when necessary.
- If a relevant test/build command is skipped because dependencies, time, or environment are unavailable, state that clearly in the final response.

## Infra Safety

- Do not run `pulumi up`, `npm run preview`, install VS Code extensions, or deploy infrastructure unless explicitly requested.
- Treat `infra/pulumi` as deployment code and `infra/site` as static assets; validate locally before proposing deployment.
