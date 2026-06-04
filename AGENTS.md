# AGENTS.md

Guidance for AI coding agents in this repository.

## User-Driven Code Style

Build and maintain an impression of the user's desired code style from prompts and feedback. This is the foremost development guideline: all other development impressions should stem from observed user preferences and durable guidance recorded here.

Learn continuously. Keep the working impression current during each task, and update Code Style Learnings when durable preferences emerge.

## Code Style Learnings

Agent-maintained. Update when prompts or feedback reveal stable, reusable code preferences.

- Prefer clearly separated, purpose-specific sections for durable guidance instead of mixing requirements with reference documentation.
- Prefer context-efficient communication and edits; compact AGENTS.md when appropriate without losing useful guidance, and avoid unnecessary detail, churn, or broad rewrites.

## Documentation Requirements

- Review and update Documentation when repository behavior changes.
- Product changes require updated feature documentation.
- Architecture changes require updated responsibilities, data flows, commands, or constraints.

## Documentation

Agent-maintained. Update when product behavior, architecture, data flows, commands, or constraints change.

### Project Overview

`rat` is an experimental semantic highlighter for Go and TypeScript with:

- A CLI and local HTTP server in `cmd/rat`.
- Shared highlighting, file loading, rendering, `gopls`, and API logic under `internal/`.
- A Lambda-oriented entry point in `cmd/highlight-lambda` and a local helper in `cmd/highlight-local`.
- A VS Code extension in `vscode-text-semantic` that consumes spans from the local `rat` server.
- Deployment/site support under `infra/`.

Core behavior is semantic highlighting, not plain syntax highlighting. Preserve spans, declaration/reference coloring, control-flow coloring, and shared output behavior across terminal, HTTP, and VS Code consumers. Go semantic coloring is the baseline: when TypeScript can express the same semantic signal with tree-sitter and same-file resolution, match Go's coloring categories and control-flow treatment. Go semantic parsing uses Go AST/type information and `gopls`; TypeScript parsing uses tree-sitter plus same-file lexical declaration/reference resolution, with embedded TypeScript LSP definition lookup for unresolved references.

TypeScript fixtures should exercise Go fixture concepts where tree-sitter same-file resolution can support them: lexical shadowing, nested block/control-flow declarations, imports, class/interface/type members, function and method parameters, destructuring bindings, catch parameters, top-level references, builtin references, comments, literals, and keyword/control-flow coloring including try/catch/finally branch coloring. Do not imply TypeScript currently has cross-file/package resolution, Go-style reference-type framing, named struct field analysis, indirect-call analysis, or typed return-error classification.

### Key Areas

- `cmd/rat/`: CLI, local server, and pipeline golden tests.
- `internal/`: shared highlighting, file loading, rendering, generic LSP, `gopls`, TypeScript parsing, and API logic.
- `testdata/`: golden outputs used by tests.
- `vscode-text-semantic/`: VS Code extension and extension tests.
- `infra/`: Pulumi deployment code and static site assets.

### API Notes

The local server exposes `POST /spans` with:

```json
{ "path": "/absolute/path/to/file.go" }
```

It returns spans grouped by 1-based line number. Preserve this shape when editing `cmd/rat`, `internal/highlightapi`, or the VS Code extension.

## Toolchain

- Go `1.26` per `go.mod` and `go.work`; workspace rooted via `go.work`.
- `github.com/stretchr/testify` is replaced with `./third_party/testify`; keep unless dependency strategy changes intentionally.
- TypeScript highlighting uses tree-sitter plus same-file lexical declaration/reference resolution and the embedded TypeScript LSP server.
- Node/npm are used for the VS Code extension and Pulumi infra.

## Commands

- Go tests: `go test ./...`
- Update Go goldens after intentional highlighting changes: `ACCEPT=1 go test ./...`
- Build CLI: `go build ./cmd/rat`
- Build embedded `gopls`: `go build -o internal/goplsbin/gopls golang.org/x/tools/gopls`
- Build main targets: `make`
- Run local server: `go run ./cmd/rat --serve --addr :8081`
- Extension tests: from `vscode-text-semantic`, run `npm test`
- Extension build: from `vscode-text-semantic`, run `npm run build`
- Pulumi TypeScript build: from `infra/pulumi`, run `npm run build`

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

## Infra Safety

- Do not run `pulumi up`, `npm run preview`, install VS Code extensions, or deploy infrastructure unless explicitly requested.
- Treat `infra/pulumi` as deployment code and `infra/site` as static assets; validate locally before proposing deployment.
