all: internal/goplsbin/gopls
	bash -c 'go build ./cmd/rat && mv rat "$$HOME/bin"'
	bash -c 'cd vscode-text-semantic && npm run build && mv *.vsix ../'

internal/goplsbin/gopls:
	go build -o $@ golang.org/x/tools/gopls
