.PHONY: all install

all: internal/goplsbin/gopls
	bash -c 'go build ./cmd/rat'
	bash -c 'cd vscode-text-semantic && npm run build && mv *.vsix ../'
	bash -c 'PATH="$$PWD:$$PATH" go run github.com/charmbracelet/freeze@v0.2.2 --execute "rat ./cmd/rat/main.go" --output .images/cli.png --width 120 --height 44'

install: all
	bash -c 'mv rat "$$HOME/bin"'
	bash -c 'code --install-extension ./text-semantic-highlight-0.0.4.vsix'

internal/goplsbin/gopls:
	go build -o $@ golang.org/x/tools/gopls
