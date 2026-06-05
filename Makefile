.PHONY: all install

all: internal/goplsbin/gopls internal/tsgobin/tsgo
	bash -c 'go build ./cmd/rat'
	bash -c 'cd vscode-text-semantic && npm run build && mv *.vsix ../'

install: all
	bash -c 'mv rat "$$HOME/bin"'
	bash -c 'code --install-extension ./text-semantic-highlight-0.0.4.vsix'

internal/goplsbin/gopls:
	go build -o $@ golang.org/x/tools/gopls

internal/tsgobin/tsgo:
	go build -o $@ github.com/microsoft/typescript-go/cmd/tsgo