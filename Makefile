.PHONY: all install

GOPLS_EMBED_PATH := internal/file/scan/golang/goplsclient/gopls

all: $(GOPLS_EMBED_PATH)
	bash -c 'go build ./cmd/rat'
	bash -c 'cd vscode-text-semantic && npm run build && mv *.vsix ../'

install: all
	bash -c 'mv rat "$$HOME/bin"'
	bash -c 'code --install-extension ./text-semantic-highlight-0.0.4.vsix'


$(GOPLS_EMBED_PATH):
	go build -o $@ golang.org/x/tools/gopls
