# rat

You've seen cat, you've seen bat, but have you seen rat?

`rat` is an experimental semantic highlighter for Go. It colors references based on their declaration location. It also colors block keywords `if` `for` `switch` `select` to provide some a short-hand on what it's doing.

You can run: `rat main.go` and it will semantically highlight the file.

## Motivation

I want better tooling that tells me more about what I'm looking at. Syntax highlighting is nice, but it only colors keywords. What a keyword does is generally obvious at the location so it doesn't add additional information. It's more like redundancy rather than adding additional information to the page. This aims to add extra information to the page to clarify where different references are coming from and also providing some context on the type of block the reader is looking at.

### Reference coloring

Coloring references is important to me, I believe you can get a feel for the complexity and purpose of something based on its integrations.

* Externally declared: Purple
* Same Repository: Blue
* Same Package: Light Blue
* Same File: Green
* Parameter: Orange
* Same Function: Yellow

### Block coloring

`if`:
- Brown: If it's a guard-like clause. I.e. it has a definite `return`, `continue`, `break`.
- Blue: If the block exits normally.

`for`
<Fill this in for me>

`switch`
<Fill this in for me>

`select`
<Fill this in for me>

<etc.>

### Indirect calls

If you're calling a method on an interface or a function pointer, I color those Magenta because that can be anything. It's not easy to statically reason about the concrete type's location that is going to show up here. I think it's reasonable to put a little bit of a warning here.

## How It Works

I vibe coded this, so you can ask copilot. 

## Requirements

- Go 1.26 or newer, matching this repo's `go.mod`.
- Node.js and npm if you want to build the VS Code extension package.
- VS Code if you want editor decorations.

## Install The CLI and Build the VS Code Plugin

1. Make sure `$HOME/bin` is on your `PATH`:

```bash
mkdir $HOME/bin
export PATH="$HOME/bin:$PATH"
```

2. In the vscode-text-semantic directory, you want to install the dependency.
    
```bash
cd vscode-text-semantic
npm install
cd ..
```

3. This builds `rat` and the vscode plugin, and moves the final binary to `$HOME/bin/rat`.

```bash
make
```

4. You can install the plugin by opening vscode and right clicking the generated .vsix file and selecting `Install Extension VSIX`.

## Use The CLI

Print ANSI-colored output for a Go file:

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

The server accepts `POST /spans` with JSON like this:

```json
{ "path": "/absolute/path/to/file.go" }
```

It returns semantic spans that the editor extension turns into decorations.

## Install The VS Code Extension

Build the extension package:

```bash
cd vscode-text-semantic
npm install
npm run build
```

That creates a `.vsix` file in `vscode-text-semantic/`. Install it with VS Code:

```bash
code --install-extension vscode-text-semantic/text-semantic-highlight-*.vsix
```

If you use `make`, the extension package is also built and moved to the repository root.

## Issues, next steps.

Apologies if this program is kinda bad, please contribute back to make it better. You can vibe code it, I don't really care. Please don't put anything absolutely insane into it.

Some future directions are transitioning from the Go ast library to something slightly more generic like tree sitter. (Tree sitter doesn't automatically mean things will be supported, but you can reuse graph algorithms so things might look more uniform).

I think sticky scroll doesn't work very well because it eliminates the highlight.