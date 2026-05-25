all:
	bash -c 'go build ./cmd/rat && mv rat "$$HOME/bin"'
	bash -c 'cd vscode-text-semantic && npm run build && mv *.vsix ../'
